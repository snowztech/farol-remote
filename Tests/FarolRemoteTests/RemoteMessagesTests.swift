import Foundation
import Testing
@testable import FarolRemote

private func roundTrip<T: Codable & Equatable>(_ value: T) throws -> T {
    try JSONDecoder().decode(T.self, from: JSONEncoder().encode(value))
}

@Test func commandsSurviveTheWire() throws {
    let session = UUID()
    for action: RemoteCommand.Action in [
        .hello(version: RemoteProtocol.version), .watch(session: session), .watch(session: nil),
        .send(session: session, text: "yes, and run the tests\n"), .key(session: session, key: .interrupt),
        .notify(token: Data([0xAB, 0x01]), sandbox: true), .notify(token: Data([0xAB]), sandbox: false, notices: [.waiting]),
        .notify(token: Data([0xAB]), sandbox: false, notices: []), .notify(token: Data([1]), sandbox: true, notices: [.done], key: Data([7])),
        .history(session: session), .type(session: session, text: "/"), .key(session: session, key: .backspace),
    ] {
        let command = RemoteCommand(action)
        #expect(try roundTrip(command) == command)
    }
}

@Test func eventsSurviveTheWire() throws {
    let session = RemoteSession(id: UUID(), name: "Fix flaky CI tests", branch: "fix/flaky-tests", status: .waiting)
    let command = UUID()
    for event: RemoteEvent in [
        .hello(version: 1), .sessions([session]), .sessions([]), .screen(session: session.id, text: "❯ No, exit\n"),
        .done(command: command), .refused(command: command, reason: "Remote is off."), .history(session: session.id, text: "earlier\nlines"),
    ] {
        #expect(try roundTrip(event) == event)
    }
}

@Test func aMessageFromANewerVersionIsNotMistakenForAnother() {
    let unknown = Data(#"{"rename":{"session":"x","name":"y"}}"#.utf8)
    #expect(throws: (any Error).self) { try JSONDecoder().decode(RemoteEvent.self, from: unknown) }
    #expect(RemoteProtocol.accepts(RemoteProtocol.version))
    #expect(!RemoteProtocol.accepts(RemoteProtocol.version + 1))
}

@Test func aCommandThatArrivesTwiceCountsOnce() {
    var seen = SeenCommands()
    let id = UUID()
    let results = [seen.insert(id), seen.insert(id), seen.insert(UUID())]
    #expect(results == [true, false, true])
}

@Test func onlyTheLastFewCommandsAreRemembered() {
    var seen = SeenCommands(limit: 2)
    let first = UUID()
    // The oldest is dropped to stay within the limit, so it counts as new again.
    let results = [seen.insert(first), seen.insert(UUID()), seen.insert(UUID()), seen.insert(first)]
    #expect(results == [true, true, true, true])
}

@Test func anOlderPhoneThatNamesNoNoticesStillReads() throws {
    let old = Data(#"{"id":"6F0C6F5E-4C7C-4B62-9D5B-0E5B0C1E2A11","action":{"notify":{"token":"qw==","sandbox":false}}}"#.utf8)
    let command = try JSONDecoder().decode(RemoteCommand.self, from: old)
    #expect(command.action == .notify(token: Data([0xAB]), sandbox: false, notices: nil))
}

@Test func onlyTheKeyOpensWhatANotificationIsAbout() throws {
    let key = NoticeBox.newKey()
    let content = RemoteNoticeContent(session: UUID(), name: "Fix flaky CI tests", notice: .waiting)
    let sealed = try #require(NoticeBox.seal(content, key: key))
    #expect(NoticeBox.open(sealed, key: key) == content)
    #expect(NoticeBox.open(sealed, key: NoticeBox.newKey()) == nil)
    #expect(NoticeBox.open(Data("not sealed".utf8), key: key) == nil)
    #expect(NoticeBox.seal(content, key: Data([1, 2, 3])) == nil)
    // What goes through the relay does not carry the name in the clear.
    #expect(sealed.range(of: Data("flaky".utf8)) == nil)
}

@Test func aPushRequestCarriesWhatIsSealed() throws {
    let message = RelayFrame.push(token: Data([0xAB]), sandbox: false, notice: .done, sealed: Data([1, 2, 3]))
    let request = try JSONDecoder().decode([String: String].self, from: message.dropFirst())
    #expect(request["sealed"] == "AQID")
}
