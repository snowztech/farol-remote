import CryptoKit
import Foundation

/// A device's own key, made once and kept in its Keychain. The public half is how the other side knows it.
public struct DeviceKey: Sendable {
    let secret: Curve25519.KeyAgreement.PrivateKey

    public init() {
        secret = Curve25519.KeyAgreement.PrivateKey()
    }

    /// From what `stored` gave back.
    public init(stored: Data) throws {
        secret = try Curve25519.KeyAgreement.PrivateKey(rawRepresentation: stored)
    }

    /// What goes in the Keychain. Never leaves the device.
    public var stored: Data { secret.rawRepresentation }

    public var publicKey: Data { secret.publicKey.rawRepresentation }
}

public enum RemoteError: Error, Equatable {
    case offerExpired
    /// Not the phone this offer was shown to, or not a device this Mac paired.
    case unknownDevice
    /// A message that this connection did not make, in this order.
    case notFromThisConnection
}

/// What the QR code carries from the Mac to the phone. Seeing it is what proves the phone was in front of the Mac.
public struct PairingOffer: Codable, Equatable, Sendable {
    public var relay: URL
    /// The Mac's public key, so the phone knows who it talks to from the first message.
    public var mac: Data
    /// Made for this one code. It never goes to the relay, so the relay cannot pair a device of its own.
    public var secret: Data
    public var expires: Date

    public init(relay: URL, mac: DeviceKey, lasting seconds: TimeInterval = 120, now: Date = Date()) {
        self.relay = relay
        self.mac = mac.publicKey
        secret = SymmetricKey(size: .bits256).withUnsafeBytes { Data($0) }
        expires = now.addingTimeInterval(seconds)
    }

    /// The offer rides after the `#`, the part of a link a browser never sends to the server.
    public func link(on page: URL) -> URL? {
        guard let json = try? JSONEncoder().encode(self) else { return nil }
        var parts = URLComponents(url: page, resolvingAgainstBaseURL: false)
        parts?.fragment = json.base64EncodedString()
            .replacingOccurrences(of: "+", with: "-").replacingOccurrences(of: "/", with: "_")
            .replacingOccurrences(of: "=", with: "")
        return parts?.url
    }

    public init?(link: URL) {
        guard var text = URLComponents(url: link, resolvingAgainstBaseURL: false)?.fragment else { return nil }
        text = text.replacingOccurrences(of: "-", with: "+").replacingOccurrences(of: "_", with: "/")
        text += String(repeating: "=", count: (4 - text.count % 4) % 4)
        guard let json = Data(base64Encoded: text),
              let offer = try? JSONDecoder().decode(PairingOffer.self, from: json) else { return nil }
        self = offer
    }
}

/// What the phone sends back through the relay. The relay sees a public key and bytes it cannot open.
public struct PairingRequest: Codable, Equatable, Sendable {
    public var device: Data
    var encapsulated: Data
    var sealed: Data
}

/// A phone this Mac lets in.
public struct PairedDevice: Codable, Equatable, Sendable {
    public var key: Data
    public var name: String

    public init(key: Data, name: String) {
        self.key = key
        self.name = name
    }
}

/// Pairing is one sealed message, in HPKE's mode that proves two things at once:
/// the sender holds the key it claims, and it knows the secret from the QR code.
public enum Pairing {
    static let suite = HPKE.Ciphersuite.Curve25519_SHA256_ChachaPoly
    private static let info = Data("farol pair 1".utf8)
    private static let secretName = Data("farol pair secret".utf8)

    /// The phone's side, after scanning.
    public static func request(for offer: PairingOffer, from phone: DeviceKey, named name: String, now: Date = Date()) throws -> PairingRequest {
        guard now < offer.expires else { throw RemoteError.offerExpired }
        var sender = try HPKE.Sender(
            recipientKey: Curve25519.KeyAgreement.PublicKey(rawRepresentation: offer.mac), ciphersuite: suite, info: info,
            authenticatedBy: phone.secret, presharedKey: SymmetricKey(data: offer.secret), presharedKeyIdentifier: secretName)
        let sealed = try sender.seal(Data(name.utf8))
        return PairingRequest(device: phone.publicKey, encapsulated: sender.encapsulatedKey, sealed: sealed)
    }

    /// The Mac's side. Throws for a request that was not made from this offer, by the key it names.
    public static func accept(_ request: PairingRequest, for offer: PairingOffer, as mac: DeviceKey, now: Date = Date()) throws -> PairedDevice {
        guard now < offer.expires else { throw RemoteError.offerExpired }
        do {
            var recipient = try HPKE.Recipient(
                privateKey: mac.secret, ciphersuite: suite, info: info, encapsulatedKey: request.encapsulated,
                authenticatedBy: Curve25519.KeyAgreement.PublicKey(rawRepresentation: request.device),
                presharedKey: SymmetricKey(data: offer.secret), presharedKeyIdentifier: secretName)
            let name = try recipient.open(request.sealed)
            return PairedDevice(key: request.device, name: String(decoding: name, as: UTF8.self))
        } catch {
            throw RemoteError.unknownDevice
        }
    }
}
