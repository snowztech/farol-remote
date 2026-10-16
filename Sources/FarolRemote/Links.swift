import CryptoKit
import Foundation

/// What travels through the relay between a Mac and a phone. Only `sealed` carries anything private.
public enum WireFrame: Codable, Equatable, Sendable {
    case pair(PairingRequest)
    case paired
    case refused(String)
    case hello(ChannelHello)
    /// What `SecureChannel.connect` returned, which lets the other side open what we seal.
    case key(Data)
    case sealed(Data)
}

/// The relay's own wrapping, one byte that says what a message is. It matches `relay/main.go`.
public enum RelayFrame: Equatable, Sendable {
    /// Bytes for the other side. With the Mac, the phone's number rides along.
    case data(phone: UInt32?, Data)
    case joined(UInt32)
    case left(UInt32)
    case online
    case offline

    /// The room a Mac and its phones meet in: the hash of the Mac's public key, which only they know.
    public static func room(for mac: Data) -> String {
        SHA256.hash(data: mac).map { String(format: "%02x", $0) }.joined()
    }

    /// What a phone sends, and what a Mac sends to one phone.
    public static func encode(_ payload: Data, to phone: UInt32? = nil) -> Data {
        var message = Data([0])
        if let phone { message.append(contentsOf: withUnsafeBytes(of: phone.bigEndian) { Data($0) }) }
        return message + payload
    }

    /// What a Mac sends to have Apple notify one of its phones. The relay holds the key Apple asks for, and writes the words.
    /// `sealed` is which session it is about, from `NoticeBox`, which the relay passes on without being able to read.
    public static func push(token: Data, sandbox: Bool, notice: RemoteNotice, sealed: Data? = nil) -> Data {
        var request = ["token": token.map { String(format: "%02x", $0) }.joined(), "notice": notice.rawValue, "sandbox": sandbox ? "1" : ""]
        if let sealed { request["sealed"] = sealed.base64EncodedString() }
        return Data([5]) + ((try? JSONEncoder().encode(request)) ?? Data())
    }

    /// What a Mac asks of a Live Activity. `start` goes to the phone's start address, the others to the activity's own.
    public enum ActivityEvent: String, Sendable {
        case start, update, end
    }

    /// What a Mac sends to start, update or end the Live Activity on one of its phones. The relay writes the push from the counts and passes `sealed` on.
    /// `urgent` is for an agent that just started waiting: Apple caps urgent pushes, so the rest go when the phone next wakes.
    public static func activity(token: Data, sandbox: Bool, event: ActivityEvent, urgent: Bool, state: RemoteActivityState) -> Data {
        struct Request: Encodable {
            var token, sandbox, activity: String
            var waiting, working: Int
            var urgent: Bool
            var sealed: String?
        }
        let request = Request(token: token.map { String(format: "%02x", $0) }.joined(), sandbox: sandbox ? "1" : "", activity: event.rawValue,
                              waiting: state.waiting, working: state.working, urgent: urgent, sealed: state.sealed)
        return Data([5]) + ((try? JSONEncoder().encode(request)) ?? Data())
    }

    /// `side` is who reads: what the Mac gets carries a phone's number, what a phone gets does not.
    public init?(_ message: Data, readBy side: SecureChannel.Side) {
        guard let kind = message.first else { return nil }
        let rest = message.dropFirst()
        func number() -> UInt32? {
            rest.count >= 4 ? rest.prefix(4).reduce(0) { $0 << 8 | UInt32($1) } : nil
        }
        switch (kind, side) {
        case (0, .phone): self = .data(phone: nil, Data(rest))
        case (0, .mac): guard let phone = number() else { return nil }; self = .data(phone: phone, Data(rest.dropFirst(4)))
        case (1, .mac): guard let phone = number() else { return nil }; self = .joined(phone)
        case (2, .mac): guard let phone = number() else { return nil }; self = .left(phone)
        case (3, .phone): self = .online
        case (4, .phone): self = .offline
        default: return nil
        }
    }
}

/// The Mac's end of the conversation with one phone, from its first message to its commands.
/// It knows nothing of sockets: frames come in, and what to do about them comes out.
public struct MacLink {
    public enum Output: Equatable {
        case send(WireFrame)
        /// A phone scanned the code. The offer is spent, and the device is to be remembered.
        case paired(PairedDevice)
        /// The connection is encrypted both ways, by this device.
        case ready(PairedDevice)
        case command(RemoteCommand)
        /// Something did not add up, and this connection is over.
        case close
    }

    private let mac: DeviceKey
    private var channel: SecureChannel?
    private var device: PairedDevice?
    private var open = false

    public init(mac: DeviceKey) {
        self.mac = mac
    }

    /// `devices` are the phones paired so far, and `offer` the code on screen, if one is.
    public mutating func receive(_ frame: WireFrame, devices: [PairedDevice], offer: PairingOffer?, now: Date = Date()) -> [Output] {
        switch frame {
        case .pair(let request):
            guard let offer, let device = try? Pairing.accept(request, for: offer, as: mac, now: now) else {
                return [.send(.refused("This code is no longer valid. Show a new one on the Mac.")), .close]
            }
            return [.paired(device), .send(.paired)]
        case .hello(let hello):
            var channel = SecureChannel(mac, side: .mac)
            guard let device = devices.first(where: { $0.key == hello.key }),
                  let key = try? channel.connect(to: hello, trusting: [device.key]) else {
                return [.send(.refused("This device is not paired with this Mac.")), .close]
            }
            self.channel = channel
            self.device = device
            return [.send(.hello(channel.hello)), .send(.key(key))]
        case .key(let key):
            guard let device, (try? channel?.accept(key)) != nil else { return [.close] }
            open = true
            return [.ready(device)]
        case .sealed(let sealed):
            guard open, let plain = try? channel?.open(sealed) else { return [.close] }
            // One that opens and does not read is from a phone of another version, newer or one that still sends what was removed.
            // It is told so, so it does not wait for an answer.
            guard let command = try? JSONDecoder().decode(RemoteCommand.self, from: plain) else {
                struct Unknown: Decodable { var id: UUID }
                guard let id = (try? JSONDecoder().decode(Unknown.self, from: plain))?.id,
                      let answer = seal(.refused(command: id, reason: RemoteProtocol.mismatch)) else { return [] }
                return [.send(answer)]
            }
            return [.command(command)]
        case .paired, .refused:
            return [.close]
        }
    }

    /// Nil until the connection is ready.
    public mutating func seal(_ event: RemoteEvent) -> WireFrame? {
        guard open, let plain = try? JSONEncoder().encode(event), let sealed = try? channel?.seal(plain) else { return nil }
        return .sealed(sealed)
    }
}

/// The phone's end, the mirror of `MacLink`.
public struct PhoneLink {
    public enum Output: Equatable {
        case send(WireFrame)
        case paired
        case ready
        case event(RemoteEvent)
        /// The Mac said no, in words for the person holding the phone.
        case refused(String)
        case close
    }

    private let phone: DeviceKey
    private let mac: Data
    private var channel: SecureChannel
    private var sent = false
    private var open = false

    /// `mac` is the Mac's public key, from the code that was scanned.
    public init(phone: DeviceKey, mac: Data) {
        self.phone = phone
        self.mac = mac
        channel = SecureChannel(phone, side: .phone)
    }

    /// What to send once the relay says the Mac is there. `pairing` goes first, the one time a code was scanned.
    public func start(pairing: PairingRequest? = nil) -> [WireFrame] {
        (pairing.map { [.pair($0)] } ?? []) + [.hello(channel.hello)]
    }

    public mutating func receive(_ frame: WireFrame) -> [Output] {
        switch frame {
        case .paired:
            return [.paired]
        case .refused(let why):
            return [.refused(why), .close]
        case .hello(let hello):
            guard let key = try? channel.connect(to: hello, trusting: [mac]) else { return [.close] }
            sent = true
            return [.send(.key(key))]
        case .key(let key):
            guard sent, (try? channel.accept(key)) != nil else { return [.close] }
            open = true
            return [.ready]
        case .sealed(let sealed):
            guard open, let plain = try? channel.open(sealed) else { return [.close] }
            // One that opens and does not read is from a newer Mac, and is left alone.
            guard let event = try? JSONDecoder().decode(RemoteEvent.self, from: plain) else { return [] }
            return [.event(event)]
        case .pair:
            return [.close]
        }
    }

    /// Nil until the connection is ready.
    public mutating func seal(_ command: RemoteCommand) -> WireFrame? {
        (try? JSONEncoder().encode(command)).flatMap { sealRaw($0) }
    }

    mutating func sealRaw(_ plain: Data) -> WireFrame? {
        guard open, let sealed = try? channel.seal(plain) else { return nil }
        return .sealed(sealed)
    }
}
