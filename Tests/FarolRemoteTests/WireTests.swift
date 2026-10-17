import Foundation
import Testing
@testable import FarolRemote

/// What farol-host's tests read to prove it understands every command and key a phone sends, and writes events as a phone reads them.
private struct Wire: Codable {
    var keys: [String]
    var commands: [String]
    var events: [String]
}

/// `FAROL_WRITE_WIRE=1 swift test` writes host/testdata/wire.json again after a message changes.
@Test func theHostsCopyOfTheWireIsCurrent() throws {
    let file = URL(fileURLWithPath: #filePath).deletingLastPathComponent().appendingPathComponent("../../host/testdata/wire.json").standardized
    let session = UUID(uuidString: "11111111-2222-3333-4444-555555555555")!
    let command = UUID(uuidString: "AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE")!
    let attachment = UUID(uuidString: "99999999-8888-7777-6666-555555555555")!
    let commands: [RemoteCommand.Action] = [
        .hello(version: RemoteProtocol.version), .watch(session: session), .send(session: session, text: "go on"),
        .key(session: session, key: .enter), .type(session: session, text: "/"),
        .notify(token: Data([1]), sandbox: true, notices: [.waiting], key: Data([2]), starts: Data([3])), .activity(token: Data([4]), sandbox: false),
        .history(session: session), .options, .task(project: "p", agent: "a", text: "t"), .open(project: "/home/me/shelf", agent: "claude"),
        .close(session: session), .changes(session: session), .diff(session: session, path: "b.go"),
        .conversation(session: session, before: "a1.0"), .answer(session: session, request: "toolu_01", choice: 2),
        .terminal(session: session, open: true), .control(session: session, key: "d"), .commands(session: session),
        .attach(session: session, attachment: attachment, part: 0, parts: 1, data: Data([0xFF])), .forget,
    ]
    let events: [RemoteEvent] = [
        .options(projects: [RemoteChoice(id: "/home/me/shelf", name: "shelf")], agents: [RemoteChoice(id: "claude", name: "Claude Code")], agent: "claude"),
        .opened(command: command, session: session),
        .changes(session: session, scope: "Uncommitted", files: [
            RemoteChange(path: "b.go", oldPath: "a.go", status: .renamed, added: 1, removed: 2),
            RemoteChange(path: "logo.png", status: .added, added: 0, removed: 0, binary: true),
        ]),
        .conversation(session: session, messages: [
            RemoteMessage(id: "a1.0", role: .agent, text: "Done."),
            RemoteMessage(id: "a2.0", role: .tool, text: "npm test", tool: "Bash", detail: "npm test"),
        ], place: .after("u1.0"), more: false),
        .conversation(session: session, messages: [], place: .latest, more: true),
        .diff(session: session, path: "b.go", text: "@@ -1 +1 @@\n-a\n+b\n"),
        .request(session: session, request: RemoteRequest(id: "toolu_01", tool: nil, title: "Which test?",
                                                          choices: [.init(label: "store", detail: "the order test"), .init(label: "all")])),
        .request(session: session, request: nil),
        .attached(command: command, attachment: attachment, path: "/home/me/.cache/Farol/Attachments/a.png"),
        .commands(session: session, commands: [RemoteAgentCommand(name: "clear", detail: "Start over"), RemoteAgentCommand(name: "deploy", menu: true, mention: true)]),
    ]
    let line = JSONEncoder()
    line.outputFormatting = [.sortedKeys]
    let wire = Wire(keys: RemoteKey.allCases.map(\.rawValue),
                    commands: try commands.map { String(decoding: try line.encode(RemoteCommand(id: command, $0)), as: UTF8.self) },
                    events: try events.map { String(decoding: try line.encode($0), as: UTF8.self) })
    let whole = JSONEncoder()
    whole.outputFormatting = [.sortedKeys, .prettyPrinted, .withoutEscapingSlashes]
    let written = try whole.encode(wire)
    if ProcessInfo.processInfo.environment["FAROL_WRITE_WIRE"] != nil { try written.write(to: file) }
    #expect((try? Data(contentsOf: file)) == written, "host/testdata/wire.json is out of date. Run FAROL_WRITE_WIRE=1 swift test")
}
