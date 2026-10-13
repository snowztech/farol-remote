import CryptoKit
import Foundation

/// The first thing each side sends on a new connection, in the clear: who it is, and a number made for this connection.
public struct ChannelHello: Codable, Equatable, Sendable {
    public var key: Data
    public var nonce: Data
}

/// One side of an encrypted connection between a Mac and a phone it paired. Each direction is an HPKE stream sealed to the other side's key and signed with ours.
/// Both streams are tied to the two numbers from the hellos, so a recording of an older connection fails to open.
/// Messages open only in the order they were sealed, which a WebSocket keeps. One that fails to open ends the connection.
public struct SecureChannel {
    public enum Side: String, Sendable { case mac, phone }

    public let hello: ChannelHello
    private let me: DeviceKey
    private let side: Side
    private var peer: ChannelHello?
    private var sender: HPKE.Sender?
    private var recipient: HPKE.Recipient?

    public init(_ me: DeviceKey, side: Side) {
        self.me = me
        self.side = side
        hello = ChannelHello(key: me.publicKey, nonce: SymmetricKey(size: .bits256).withUnsafeBytes { Data($0) })
    }

    /// After the other side's hello. Returns what to send next, which lets the other side open what we seal.
    /// `trusted` is the keys we paired: the Mac passes its devices, the phone the one Mac it scanned.
    public mutating func connect(to peer: ChannelHello, trusting trusted: [Data]) throws -> Data {
        guard trusted.contains(peer.key) else { throw RemoteError.unknownDevice }
        let sender = try HPKE.Sender(
            recipientKey: Curve25519.KeyAgreement.PublicKey(rawRepresentation: peer.key), ciphersuite: Pairing.suite,
            info: info(from: side, with: peer), authenticatedBy: me.secret)
        self.peer = peer
        self.sender = sender
        return sender.encapsulatedKey
    }

    /// What the other side's `connect` returned.
    public mutating func accept(_ encapsulated: Data) throws {
        guard let peer else { throw RemoteError.notFromThisConnection }
        do {
            recipient = try HPKE.Recipient(
                privateKey: me.secret, ciphersuite: Pairing.suite, info: info(from: side == .mac ? .phone : .mac, with: peer),
                encapsulatedKey: encapsulated, authenticatedBy: Curve25519.KeyAgreement.PublicKey(rawRepresentation: peer.key))
        } catch {
            throw RemoteError.notFromThisConnection
        }
    }

    public mutating func seal(_ message: Data) throws -> Data {
        guard sender != nil else { throw RemoteError.notFromThisConnection }
        return try sender!.seal(message)
    }

    public mutating func open(_ sealed: Data) throws -> Data {
        guard recipient != nil else { throw RemoteError.notFromThisConnection }
        do {
            return try recipient!.open(sealed)
        } catch {
            throw RemoteError.notFromThisConnection
        }
    }

    /// The same bytes on both ends for one direction: who writes, then the Mac's number, then the phone's.
    private func info(from writer: Side, with peer: ChannelHello) -> Data {
        let (mac, phone) = side == .mac ? (hello.nonce, peer.nonce) : (peer.nonce, hello.nonce)
        return Data("farol remote 1 \(writer.rawValue)".utf8) + mac + phone
    }
}
