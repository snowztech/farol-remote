import CryptoKit
import Foundation

/// What a notification says once the phone has opened it: which session, and its name.
public struct RemoteNoticeContent: Codable, Equatable, Sendable {
    public var session: UUID
    public var name: String
    public var notice: RemoteNotice

    public init(session: UUID, name: String, notice: RemoteNotice) {
        self.session = session
        self.name = name
        self.notice = notice
    }
}

/// Seals what a notification is about with a key only the Mac and one phone hold.
/// A notification goes through the relay and Apple with no connection open, so it cannot use the channel's own encryption.
public enum NoticeBox {
    public static func newKey() -> Data {
        SymmetricKey(size: .bits256).withUnsafeBytes { Data($0) }
    }

    /// Anything only the phone should read: a notification's session, or a Live Activity's sessions.
    public static func seal<Content: Encodable>(_ content: Content, key: Data) -> Data? {
        guard key.count == 32, let plain = try? JSONEncoder().encode(content) else { return nil }
        return try? ChaChaPoly.seal(plain, using: SymmetricKey(data: key)).combined
    }

    public static func open<Content: Decodable>(_ sealed: Data, as type: Content.Type, key: Data) -> Content? {
        guard key.count == 32, let box = try? ChaChaPoly.SealedBox(combined: sealed),
              let plain = try? ChaChaPoly.open(box, using: SymmetricKey(data: key)) else { return nil }
        return try? JSONDecoder().decode(type, from: plain)
    }

    public static func open(_ sealed: Data, key: Data) -> RemoteNoticeContent? {
        open(sealed, as: RemoteNoticeContent.self, key: key)
    }

    #if os(iOS)
    /// The phone's key, in the Keychain group the app shares with its notification extension. Made the first time it is asked for.
    /// Readable while the phone is locked, since that is when notifications arrive.
    public static func storedKey(create: Bool) -> Data? {
        let query: [CFString: Any] = [kSecClass: kSecClassGenericPassword, kSecAttrService: "Farol Remote", kSecAttrAccount: "notices"]
        var found: CFTypeRef?
        if SecItemCopyMatching(query.merging([kSecReturnData: true]) { $1 } as CFDictionary, &found) == errSecSuccess, let key = found as? Data {
            return key
        }
        guard create else { return nil }
        let key = newKey()
        let item = query.merging([kSecValueData: key, kSecAttrAccessible: kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly]) { $1 }
        return SecItemAdd(item as CFDictionary, nil) == errSecSuccess ? key : nil
    }
    #endif
}
