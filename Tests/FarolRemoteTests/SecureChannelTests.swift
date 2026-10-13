import Foundation
import Testing
@testable import FarolRemote

private let relay = URL(string: "wss://relay.farol.sh")!

// MARK: Pairing

@Test func aPhoneThatScannedTheCodeIsPaired() throws {
    let mac = DeviceKey(), phone = DeviceKey()
    let offer = PairingOffer(relay: relay, mac: mac)
    let request = try Pairing.request(for: offer, from: phone, named: "Lucas's iPhone")
    #expect(try Pairing.accept(request, for: offer, as: mac) == PairedDevice(key: phone.publicKey, name: "Lucas's iPhone"))
}

@Test func theOfferSurvivesTheQRCode() throws {
    let offer = PairingOffer(relay: relay, mac: DeviceKey())
    let link = try #require(offer.link(on: URL(string: "https://remote.farol.sh/pair")!))
    // Everything secret is after the #, which never reaches a server.
    #expect(link.absoluteString.hasPrefix("https://remote.farol.sh/pair#"))
    #expect(PairingOffer(link: link) == offer)
    #expect(PairingOffer(link: URL(string: "https://remote.farol.sh/pair#nonsense")!) == nil)
}

@Test func aDeviceThatNeverSawTheCodeIsRefused() throws {
    let mac = DeviceKey()
    let shown = PairingOffer(relay: relay, mac: mac)
    // The relay knows the Mac's key and the time, and nothing else.
    var guessed = shown
    guessed.secret = Data(repeating: 7, count: 32)
    let request = try Pairing.request(for: guessed, from: DeviceKey(), named: "Not yours")
    #expect(throws: RemoteError.unknownDevice) { try Pairing.accept(request, for: shown, as: mac) }
}

@Test func aRequestCannotBePassedOffAsAnotherDevice() throws {
    let mac = DeviceKey()
    let offer = PairingOffer(relay: relay, mac: mac)
    var request = try Pairing.request(for: offer, from: DeviceKey(), named: "Lucas's iPhone")
    request.device = DeviceKey().publicKey
    #expect(throws: RemoteError.unknownDevice) { try Pairing.accept(request, for: offer, as: mac) }
}

@Test func anOldCodeNoLongerPairs() throws {
    let mac = DeviceKey(), phone = DeviceKey()
    let start = Date()
    let offer = PairingOffer(relay: relay, mac: mac, lasting: 120, now: start)
    let request = try Pairing.request(for: offer, from: phone, named: "Lucas's iPhone", now: start)
    let late = start.addingTimeInterval(121)
    #expect(throws: RemoteError.offerExpired) { try Pairing.accept(request, for: offer, as: mac, now: late) }
    #expect(throws: RemoteError.offerExpired) { try Pairing.request(for: offer, from: phone, named: "x", now: late) }
}

@Test func aKeyComesBackFromTheKeychainTheSame() throws {
    let key = DeviceKey()
    #expect(try DeviceKey(stored: key.stored).publicKey == key.publicKey)
}

// MARK: Channel

/// A Mac and a phone that paired, with a connection set up between them.
private func connected(mac: DeviceKey = DeviceKey(), phone: DeviceKey = DeviceKey()) throws -> (mac: SecureChannel, phone: SecureChannel) {
    var onMac = SecureChannel(mac, side: .mac), onPhone = SecureChannel(phone, side: .phone)
    let fromMac = try onMac.connect(to: onPhone.hello, trusting: [phone.publicKey])
    let fromPhone = try onPhone.connect(to: onMac.hello, trusting: [mac.publicKey])
    try onMac.accept(fromPhone)
    try onPhone.accept(fromMac)
    return (onMac, onPhone)
}

@Test func bothSidesReadWhatTheOtherSeals() throws {
    var (mac, phone) = try connected()
    for text in ["yes", "", "run the tests again"] {
        let sealed = try phone.seal(Data(text.utf8))
        #expect(sealed != Data(text.utf8))
        #expect(try mac.open(sealed) == Data(text.utf8))
    }
    #expect(try phone.open(try mac.seal(Data("screen".utf8))) == Data("screen".utf8))
}

@Test func aDeviceThatIsNotPairedIsRefused() throws {
    var mac = SecureChannel(DeviceKey(), side: .mac)
    let stranger = SecureChannel(DeviceKey(), side: .phone)
    #expect(throws: RemoteError.unknownDevice) { try mac.connect(to: stranger.hello, trusting: [DeviceKey().publicKey]) }
}

@Test func aRecordingOfAnOlderConnectionDoesNotOpen() throws {
    let macKey = DeviceKey(), phoneKey = DeviceKey()
    // What the relay saw the first time: the phone's hello, what its connect returned and a sealed "press enter".
    var firstMac = SecureChannel(macKey, side: .mac), firstPhone = SecureChannel(phoneKey, side: .phone)
    _ = try firstMac.connect(to: firstPhone.hello, trusting: [phoneKey.publicKey])
    let recorded = try firstPhone.connect(to: firstMac.hello, trusting: [macKey.publicKey])
    let pressEnter = try firstPhone.seal(Data("enter".utf8))

    // Played back to the Mac on a new connection, which has a new number of its own.
    var again = SecureChannel(macKey, side: .mac)
    _ = try again.connect(to: firstPhone.hello, trusting: [phoneKey.publicKey])
    try again.accept(recorded)
    #expect(throws: RemoteError.notFromThisConnection) { try again.open(pressEnter) }
}

@Test func aMessageThatWasChangedDoesNotOpen() throws {
    var (mac, phone) = try connected()
    var changed = try phone.seal(Data("one".utf8))
    changed[changed.startIndex] ^= 1
    #expect(throws: RemoteError.notFromThisConnection) { try mac.open(changed) }
}

@Test func aMessageOpensOnlyInItsPlace() throws {
    var (mac, phone) = try connected()
    _ = try phone.seal(Data("first".utf8))
    let second = try phone.seal(Data("second".utf8))
    // The relay dropped the first one, so the second arrives where the first belongs.
    #expect(throws: RemoteError.notFromThisConnection) { try mac.open(second) }
}

@Test func whatOneSideSealsCannotBeHandedBackToIt() throws {
    var (mac, _) = try connected()
    let own = try mac.seal(Data("screen".utf8))
    #expect(throws: RemoteError.notFromThisConnection) { try mac.open(own) }
}

@Test func nothingIsSealedBeforeTheConnectionIsSetUp() {
    var mac = SecureChannel(DeviceKey(), side: .mac)
    #expect(throws: RemoteError.notFromThisConnection) { try mac.seal(Data("early".utf8)) }
    #expect(throws: RemoteError.notFromThisConnection) { try mac.open(Data("early".utf8)) }
    #expect(throws: RemoteError.notFromThisConnection) { try mac.accept(Data(repeating: 0, count: 32)) }
}
