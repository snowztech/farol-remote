import Foundation
import Testing
@testable import FarolRemote

/// A Mac and a phone with the relay's job done by hand: each frame one side sends is given to the other.
private struct Pair {
    let macKey = DeviceKey(), phoneKey = DeviceKey()
    var mac: MacLink
    var phone: PhoneLink
    var devices: [PairedDevice] = []
    var offer: PairingOffer?
    var macSaw: [MacLink.Output] = []
    var phoneSaw: [PhoneLink.Output] = []

    init() {
        mac = MacLink(mac: macKey)
        phone = PhoneLink(phone: phoneKey, mac: macKey.publicKey)
    }

    /// A new connection between the same two devices, as after a dropped network.
    mutating func reconnect() {
        mac = MacLink(mac: macKey)
        phone = PhoneLink(phone: phoneKey, mac: macKey.publicKey)
        macSaw = []
        phoneSaw = []
    }

    mutating func toMac(_ frames: [WireFrame]) {
        for frame in frames {
            let outputs = mac.receive(frame, devices: devices, offer: offer)
            macSaw += outputs
            for case .paired(let device) in outputs {
                devices.append(device)
                offer = nil
            }
            toPhone(outputs.compactMap { if case .send(let frame) = $0 { frame } else { nil } })
        }
    }

    mutating func toPhone(_ frames: [WireFrame]) {
        for frame in frames {
            let outputs = phone.receive(frame)
            phoneSaw += outputs
            toMac(outputs.compactMap { if case .send(let frame) = $0 { frame } else { nil } })
        }
    }

    mutating func scanAndConnect() throws {
        let shown = PairingOffer(relay: URL(string: "wss://relay.farol.sh")!, mac: macKey)
        offer = shown
        toMac(phone.start(pairing: try Pairing.request(for: shown, from: phoneKey, named: "Lucas's iPhone")))
    }
}

@Test func scanningTheCodeEndsInAnEncryptedConnection() throws {
    var pair = Pair()
    try pair.scanAndConnect()
    let device = PairedDevice(key: pair.phoneKey.publicKey, name: "Lucas's iPhone")
    #expect(pair.devices == [device])
    #expect(pair.offer == nil)
    #expect(pair.macSaw.contains(.ready(device)))
    #expect(pair.phoneSaw.contains(.paired) && pair.phoneSaw.contains(.ready))
}

@Test func commandsAndEventsCrossOnceConnected() throws {
    var pair = Pair()
    try pair.scanAndConnect()
    let session = UUID()
    let command = RemoteCommand(.key(session: session, key: .enter))
    let sealedCommand = pair.phone.seal(command)
    pair.toMac([try #require(sealedCommand)])
    #expect(pair.macSaw.last == .command(command))

    let event = RemoteEvent.screen(session: session, text: "❯ Yes, I trust this folder")
    let sealedEvent = pair.mac.seal(event)
    pair.toPhone([try #require(sealedEvent)])
    #expect(pair.phoneSaw.last == .event(event))
}

@Test func aPairedPhoneComesBackWithoutACode() throws {
    var pair = Pair()
    try pair.scanAndConnect()
    pair.reconnect()
    pair.toMac(pair.phone.start())
    #expect(pair.macSaw.contains(.ready(pair.devices[0])))
    #expect(pair.phoneSaw.contains(.ready))
}

@Test func aPhoneThatWasRemovedIsTurnedAway() throws {
    var pair = Pair()
    try pair.scanAndConnect()
    pair.devices = []
    pair.reconnect()
    pair.toMac(pair.phone.start())
    #expect(pair.macSaw.contains(.close))
    #expect(pair.phoneSaw.contains(.refused("This device is not paired with this Mac.")))
    let afterRefusal = pair.phone.seal(RemoteCommand(.watch(session: nil)))
    #expect(afterRefusal == nil)
}

@Test func aCodeWorksOnce() throws {
    var pair = Pair()
    try pair.scanAndConnect()
    // A second device with a photo of the same code, after the first one used it.
    let late = DeviceKey()
    var mac = MacLink(mac: pair.macKey)
    let shown = PairingOffer(relay: URL(string: "wss://relay.farol.sh")!, mac: pair.macKey)
    let request = try Pairing.request(for: shown, from: late, named: "Someone else")
    let outputs = mac.receive(.pair(request), devices: pair.devices, offer: pair.offer)
    #expect(outputs == [.send(.refused("This code is no longer valid. Show a new one on the Mac.")), .close])
}

@Test func nothingIsReadBeforeTheConnectionIsReady() {
    var mac = MacLink(mac: DeviceKey())
    #expect(mac.receive(.sealed(Data("early".utf8)), devices: [], offer: nil) == [.close])
    let early = mac.seal(.sessions([]))
    #expect(early == nil)
}

@Test func theRelaysWrappingReadsBackOnBothSides() {
    let payload = Data("sealed".utf8)
    #expect(RelayFrame(RelayFrame.encode(payload), readBy: .phone) == .data(phone: nil, payload))
    #expect(RelayFrame(RelayFrame.encode(payload, to: 258), readBy: .mac) == .data(phone: 258, payload))
    #expect(RelayFrame(Data([1, 0, 0, 0, 7]), readBy: .mac) == .joined(7))
    #expect(RelayFrame(Data([2, 0, 0, 1, 0]), readBy: .mac) == .left(256))
    #expect(RelayFrame(Data([3]), readBy: .phone) == .online)
    #expect(RelayFrame(Data([4]), readBy: .phone) == .offline)
    #expect(RelayFrame(Data([1, 0]), readBy: .mac) == nil)
    #expect(RelayFrame(Data(), readBy: .phone) == nil)
    #expect(RelayFrame.room(for: Data()) == "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
}

@Test func aMessageFromANewerAppIsLeftAlone() throws {
    var pair = Pair()
    try pair.scanAndConnect()
    // Sealed by the real phone, so it opens, but it is not a command this Mac knows.
    let sealedUnknown = pair.phone.sealRaw(Data(#"{"id":"x","action":{"rename":{}}}"#.utf8))
    let unknown = try #require(sealedUnknown)
    let before = pair.macSaw.count
    pair.toMac([unknown])
    #expect(pair.macSaw.count == before)
    // The connection is still good for what it does know.
    let command = RemoteCommand(.watch(session: nil))
    let sealed = pair.phone.seal(command)
    pair.toMac([try #require(sealed)])
    #expect(pair.macSaw.last == .command(command))
}

@Test func aPushRequestCarriesTheTokenAndWhatItIsAbout() throws {
    let message = RelayFrame.push(token: Data([0xAB, 0x01, 0xFF]), sandbox: true, notice: .waiting)
    #expect(message.first == 5)
    let request = try JSONDecoder().decode([String: String].self, from: message.dropFirst())
    #expect(request == ["token": "ab01ff", "notice": "waiting", "sandbox": "1"])
}

@Test func aCommandThisMacDoesNotKnowIsRefused() throws {
    var pair = Pair()
    try pair.scanAndConnect()
    let id = UUID()
    let sealedUnknown = pair.phone.sealRaw(Data(#"{"id":"\#(id)","action":{"rename":{}}}"#.utf8))
    pair.toMac([try #require(sealedUnknown)])
    guard case .event(.refused(let command, _)) = pair.phoneSaw.last else {
        Issue.record("the phone was not told")
        return
    }
    #expect(command == id)
}
