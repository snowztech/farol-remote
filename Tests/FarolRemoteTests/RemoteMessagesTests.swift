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
        .notify(token: Data([1]), sandbox: false, starts: Data([0xCD])),
        .history(session: session), .options, .task(project: "/Users/me/shelf", agent: "claude", text: "fix the flaky test"), .type(session: session, text: "/"), .key(session: session, key: .backspace),
        .activity(token: Data([0xAB, 0x02]), sandbox: true),
        .open(project: "/Users/me/shelf", agent: "claude"), .open(project: "/Users/me/shelf", agent: nil),
        .close(session: session), .key(session: session, key: .shiftTab),
        .conversation(session: session, before: nil), .conversation(session: session, before: "a1b2.0"),
        .changes(session: session), .diff(session: session, path: "Sources/App.swift"),
        .answer(session: session, request: "toolu_01", choice: 2),
        .terminal(session: session, open: true), .control(session: session, key: "d"), .commands(session: session),
        .attach(session: session, attachment: UUID(), part: 1, parts: 3, data: Data([0xFF, 0xD8, 0xFF])),
    ] {
        let command = RemoteCommand(action)
        #expect(try roundTrip(command) == command)
    }
}

@Test func eventsSurviveTheWire() throws {
    let session = RemoteSession(id: UUID(), name: "Fix flaky CI tests", branch: "fix/flaky-tests", status: .waiting)
    let command = UUID()
    for event: RemoteEvent in [
        .hello(version: 1), .hello(version: 1, features: ["conversation", "requests"], name: "Hetzner VM"), .hello(version: 1, features: []),
        .sessions([session]), .sessions([]), .screen(session: session.id, text: "❯ No, exit\n"),
        .done(command: command), .refused(command: command, reason: "Remote is off."), .history(session: session.id, text: "earlier\nlines"),
        .options(projects: [RemoteChoice(id: "/Users/me/shelf", name: "shelf")], agents: [RemoteChoice(id: "a", name: "Claude Code")], agent: "a"),
        .opened(command: command, session: session.id),
        .changes(session: session.id, scope: "Uncommitted", files: [
            RemoteChange(path: "Sources/App.swift", status: .modified, added: 12, removed: 3),
            RemoteChange(path: "New.swift", oldPath: "Old.swift", status: .renamed, added: 0, removed: 0),
            RemoteChange(path: "icon.png", status: .added, added: 0, removed: 0, binary: true),
        ]),
        .changes(session: session.id, scope: "Since main", files: []),
        .conversation(session: session.id, messages: [], place: .after("x"), more: false),
        .conversation(session: session.id, messages: [RemoteMessage(id: "c", role: .agent, text: "Done.")], place: .before("d"), more: true),
        .conversation(session: session.id, messages: [
            RemoteMessage(id: "a", role: .you, text: "fix the flaky test"),
            RemoteMessage(id: "b", role: .tool, text: "Run the tests", tool: "Bash", detail: "npm test"),
        ], place: .latest, more: true),
        .diff(session: session.id, path: "Sources/App.swift", text: "@@ -1,2 +1,2 @@\n-old\n+new\n same"),
        .request(session: session.id, request: RemoteRequest(id: "toolu_01", tool: "Bash", title: "npm test", detail: "npm test -- --watch=false")),
        .request(session: session.id, request: RemoteRequest(id: "toolu_02", tool: nil, title: "Which database?", choices: [
            RemoteRequest.Choice(label: "SQLite", detail: "One file, nothing to run"), RemoteRequest.Choice(label: "Postgres"),
        ])),
        .request(session: session.id, request: nil),
        .attached(command: command, attachment: UUID(), path: "/Users/me/Library/Caches/Farol/Attachments/a.jpg"),
        .commands(session: session.id, commands: [
            RemoteAgentCommand(name: "compact", detail: "Summarize the conversation"), RemoteAgentCommand(name: "model", menu: true), RemoteAgentCommand(name: "farol", mention: true),
        ]),
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

@Test func aLiveActivityShowsCountsAndOnlyThePhoneReadsTheNames() throws {
    let key = NoticeBox.newKey()
    let sessions = [
        RemoteSession(id: UUID(), name: "Offline sync", branch: "feat/sync", status: .idle),
        RemoteSession(id: UUID(), name: "Add fuzzy search", branch: nil, status: .working),
        RemoteSession(id: UUID(), name: "Fix flaky CI tests", branch: nil, status: .waiting),
    ]
    let state = RemoteActivityState(sessions, key: key)
    #expect(state.waiting == 1 && state.working == 1 && !state.isQuiet)
    // The one that needs you first, then the one working. Idle ones are left out.
    #expect(state.sessions(key: key).map(\.name) == ["Fix flaky CI tests", "Add fuzzy search"])
    #expect(state.sessions(key: NoticeBox.newKey()).isEmpty)
    #expect(!(state.sealed ?? "").contains("flaky"))
    #expect(try roundTrip(state) == state)
    #expect(RemoteActivityState([], key: key).isQuiet)
}

@Test func aLiveActivityPushCarriesWhatTheRelayExpects() throws {
    let frame = RelayFrame.activity(token: Data([0xAB, 0x01]), sandbox: false, event: .end, urgent: true, state: RemoteActivityState(waiting: 2, working: 0, sealed: "c2VhbGVk"))
    #expect(frame.first == 5)
    let sent = try JSONSerialization.jsonObject(with: frame.dropFirst()) as? [String: Any]
    #expect(sent?["token"] as? String == "ab01")
    #expect(sent?["activity"] as? String == "end")
    #expect(sent?["waiting"] as? Int == 2)
    #expect(sent?["sealed"] as? String == "c2VhbGVk")
    #expect(sent?["urgent"] as? Bool == true)
}

@Test func aConversationPutsMessagesWhereTheHostSays() {
    let say = { (id: String) in RemoteMessage(id: id, role: .agent, text: id) }
    var conversation = RemoteConversation()
    func take(_ ids: [String], at place: RemotePlace, more: Bool = false) -> Bool { conversation.take(ids.map(say), at: place, more: more) }
    #expect(take(["c", "d"], at: .latest, more: true))
    #expect(conversation.hasEarlier)
    #expect(take(["a", "b"], at: .before("c")))
    #expect(take(["e"], at: .after("d")))
    #expect(conversation.messages.map(\.id) == ["a", "b", "c", "d", "e"])
    #expect(!conversation.hasEarlier)

    // A page or new messages that no longer fit, as after a rewind, change nothing.
    #expect(!take(["x"], at: .after("d")))
    #expect(!take(["x"], at: .before("b")))
    #expect(conversation.messages.map(\.id) == ["a", "b", "c", "d", "e"])

    // A rewind on the host sends the latest page instead of everything.
    #expect(take(["a", "f"], at: .latest))
    #expect(conversation.messages.map(\.id) == ["a", "f"])
}

@Test func aPictureSplitsIntoPartsThatFitAMessageAndJoinBackWhole() throws {
    let picture = Data((0..<(RemoteAttachment.partSize * 2 + 10)).map { UInt8($0 % 251) })
    let parts = RemoteAttachment.parts(of: picture)
    #expect(parts.count == 3)
    #expect(parts.last?.count == 10)
    #expect(parts.reduce(Data(), +) == picture)
    // Sealed, framed and addressed, the largest part still fits one message of the relay, with room to spare.
    let command = try JSONEncoder().encode(RemoteCommand(.attach(session: UUID(), attachment: UUID(), part: 0, parts: 3, data: parts[0])))
    let sealed = Data(count: command.count + 64)
    let message = RelayFrame.encode(try JSONEncoder().encode(WireFrame.sealed(sealed)), to: 1)
    #expect(message.count < 200_000)
}

@Test func aHostSaysWhatItCanDo() throws {
    #expect(RemoteFeature.of(["conversation", "pictures-in-3d", "terminal"]) == [.conversation, .terminal])
    #expect(RemoteFeature.of([]).isEmpty)
    #expect(RemoteFeature.of(nil) == Set(RemoteFeature.allCases))
    // A host from before features greets as it always did, and reads as one that said nothing.
    let old = try JSONDecoder().decode(RemoteEvent.self, from: Data(#"{"hello":{"version":1}}"#.utf8))
    #expect(old == .hello(version: 1))
}

@Test func farolHostsHelloReadsAsAHostThatDoesWhatAMacDoesForAgents() throws {
    // What `farol-host` writes, as its host/swift_test.go checks.
    let hello = try JSONDecoder().decode(RemoteEvent.self, from: Data(#"{"hello":{"version":1,"features":["conversation","requests","commands","attachments","terminal","notifications"],"name":"host"}}"#.utf8))
    #expect(hello == .hello(version: 1, features: ["conversation", "requests", "commands", "attachments", "terminal", "notifications"], name: "host"))
    // Everything a Mac does for agents. Starting sessions and changes are a Mac's alone for now.
    #expect(RemoteFeature.of(["conversation", "requests", "commands", "attachments", "terminal", "notifications"]) == [.conversation, .requests, .commands, .attachments, .terminal, .notifications])
}

@Test func farolHostsSealedNoticeOpensOnThePhone() throws {
    // Sealed by host/notices.go with the key 0, 1, 2 … 31.
    let sealed = try #require(Data(base64Encoded: "XY7yZl8nCg2Bdnh5HB+RRks5oaHiHWslhUrePMqqu+K3qJV4+9lvUabYuTkW5F9tjIyYs9F7RroskmPGsjA9sV6cHtl3P+x0HTvXzf7zLP2YsJpQcFr6x3cP2OvbOqgZ0LWgTiTS6sXbmsLxdx4="))
    let content = NoticeBox.open(sealed, key: Data(0..<32))
    #expect(content == RemoteNoticeContent(session: try #require(UUID(uuidString: "11111111-2222-3333-4444-555555555555")), name: "api", notice: .waiting))
}

@Test func farolHostsCommandsAndPicturesRead() throws {
    let session = try #require(UUID(uuidString: "11111111-2222-3333-4444-555555555555"))
    let picture = try #require(UUID(uuidString: "AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE"))
    let decode = { (text: String) in try JSONDecoder().decode(RemoteEvent.self, from: Data(text.utf8)) }
    let commands = try decode(#"{"commands":{"session":"11111111-2222-3333-4444-555555555555","commands":[{"name":"compact","detail":"Summarize","menu":false,"mention":false},{"name":"farol","menu":false,"mention":true}]}}"#)
    #expect(commands == .commands(session: session, commands: [RemoteAgentCommand(name: "compact", detail: "Summarize"), RemoteAgentCommand(name: "farol", mention: true)]))
    let path = "/home/me/.cache/Farol/Attachments/AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE.png"
    let attached = try decode(#"{"attached":{"command":"AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE","attachment":"AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE","path":"/home/me/.cache/Farol/Attachments/AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE.png"}}"#)
    #expect(attached == .attached(command: picture, attachment: picture, path: path))
    // A part as host_test.go sends it: the bytes in base64.
    let part = try JSONSerialization.jsonObject(with: JSONEncoder().encode(RemoteCommand(.attach(session: session, attachment: picture, part: 1, parts: 2, data: Data([1, 2, 3])))))
    let action = (part as? [String: Any])?["action"] as? [String: Any]
    #expect(NSDictionary(dictionary: action ?? [:]) == ["attach": ["session": session.uuidString, "attachment": picture.uuidString, "part": 1, "parts": 2, "data": "AQID"]])
}

@Test func farolHostsRequestsReadAndItsAnswersGoAsItReadsThem() throws {
    let session = try #require(UUID(uuidString: "11111111-2222-3333-4444-555555555555"))
    let decode = { (text: String) in try JSONDecoder().decode(RemoteEvent.self, from: Data(text.utf8)) }
    #expect(try decode(#"{"request":{"session":"11111111-2222-3333-4444-555555555555"}}"#) == .request(session: session, request: nil))
    let tool = try decode(#"{"request":{"session":"11111111-2222-3333-4444-555555555555","request":{"id":"toolu_1","tool":"Bash","title":"Run the tests","detail":"npm test","choices":[]}}}"#)
    #expect(tool == .request(session: session, request: RemoteRequest(id: "toolu_1", tool: "Bash", title: "Run the tests", detail: "npm test")))
    let question = try decode(#"{"request":{"session":"11111111-2222-3333-4444-555555555555","request":{"id":"q","title":"Which?","choices":[{"label":"A","detail":"first"},{"label":"B"}]}}}"#)
    #expect(question == .request(session: session, request: RemoteRequest(id: "q", tool: nil, title: "Which?", choices: [.init(label: "A", detail: "first"), .init(label: "B")])))
    // What host_test.go sends as the phone's answer.
    let answer = try JSONSerialization.jsonObject(with: JSONEncoder().encode(RemoteCommand(.answer(session: session, request: "toolu_1", choice: 2))))
    let action = (answer as? [String: Any])?["action"] as? [String: Any]
    #expect(NSDictionary(dictionary: action ?? [:]) == ["answer": ["session": session.uuidString, "request": "toolu_1", "choice": 2]])
}

@Test func farolHostsConversationReads() throws {
    let session = try #require(UUID(uuidString: "11111111-2222-3333-4444-555555555555"))
    let decode = { (text: String) in try JSONDecoder().decode(RemoteEvent.self, from: Data(text.utf8)) }
    let after = try decode(#"{"conversation":{"session":"11111111-2222-3333-4444-555555555555","messages":[{"id":"a1.0","role":"agent","text":"<b> & co"},{"id":"a2.0","role":"tool","text":"Run the tests","tool":"Bash","detail":"npm test"}],"place":{"after":{"_0":"u1.0"}},"more":false}}"#)
    #expect(after == .conversation(session: session, messages: [RemoteMessage(id: "a1.0", role: .agent, text: "<b> & co"),
                                                                RemoteMessage(id: "a2.0", role: .tool, text: "Run the tests", tool: "Bash", detail: "npm test")],
                                   place: .after("u1.0"), more: false))
    let latest = try decode(#"{"conversation":{"session":"11111111-2222-3333-4444-555555555555","messages":[],"place":{"latest":{}},"more":true}}"#)
    #expect(latest == .conversation(session: session, messages: [], place: .latest, more: true))
    let before = try decode(#"{"conversation":{"session":"11111111-2222-3333-4444-555555555555","messages":[],"place":{"before":{"_0":"x"}},"more":false}}"#)
    #expect(before == .conversation(session: session, messages: [], place: .before("x"), more: false))
}
