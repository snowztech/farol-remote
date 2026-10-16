import Foundation

/// What a paired phone and the Mac say to each other. Shared with the iOS app, so nothing here knows of git or AppKit.
public enum RemoteProtocol {
    /// Raised when a message changes meaning. Both sides say theirs first, and only the same number talks.
    public static let version = 1

    public static func accepts(_ other: Int) -> Bool { other == version }

    /// Why a host refuses a command it does not know. Either side may be the older one, so it asks to update both.
    public static let mismatch = "Farol on the Mac and Farol for iPhone are different versions. Update both to use this."
}

/// A key that isn't text, for a prompt that waits on Return or an arrow.
public enum RemoteKey: String, Codable, CaseIterable, Sendable {
    case enter, escape, tab, up, down, left, right, backspace
    /// Shift-Tab, which Claude Code takes to switch between its modes.
    case shiftTab
    /// Control-C.
    case interrupt
}

/// What a notification is about. The words are the relay's, so a Mac cannot make a phone show any text it likes.
public enum RemoteNotice: String, Codable, Sendable {
    case waiting, done
}

/// What the sidebar dot shows for a session.
public enum RemoteStatus: String, Codable, Sendable {
    case idle, working, waiting, done, stopped
}

/// A session as the phone lists it.
public struct RemoteSession: Codable, Equatable, Identifiable, Sendable {
    public var id: UUID
    public var name: String
    public var branch: String?
    public var status: RemoteStatus

    public init(id: UUID, name: String, branch: String?, status: RemoteStatus) {
        self.id = id
        self.name = name
        self.branch = branch
        self.status = status
    }
}

/// Where messages of a conversation go on the phone.
public enum RemotePlace: Codable, Equatable, Sendable {
    /// Instead of everything shown: the latest page, or the whole of a new branch after you rewound the conversation.
    case latest
    /// Before the message with this id, a page further back.
    case before(String)
    /// After the message with this id, which the agent just wrote.
    case after(String)
}

/// A session's conversation as far as the phone has it, put together from the host's `conversation` events.
public struct RemoteConversation: Equatable, Sendable {
    public private(set) var messages: [RemoteMessage] = []
    /// The host has messages before the first one here.
    public private(set) var hasEarlier = false

    public init() {}

    /// Puts messages where the host says they go. False when they do not fit what is here, like a page before a message
    /// that is no longer first, and then nothing changes. The phone asks for the latest page again to be sure.
    public mutating func take(_ new: [RemoteMessage], at place: RemotePlace, more: Bool) -> Bool {
        switch place {
        case .latest:
            messages = new
            hasEarlier = more
        case .before(let id):
            guard messages.first?.id == id else { return false }
            messages = new + messages
            hasEarlier = more
        case .after(let id):
            guard messages.last?.id == id else { return false }
            messages += new
        }
        return true
    }
}

/// One step of an agent's conversation, as its log records it.
public struct RemoteMessage: Codable, Equatable, Identifiable, Sendable {
    public enum Role: String, Codable, Sendable {
        /// What you wrote.
        case you
        /// What the agent answered, in Markdown.
        case agent
        /// A tool the agent ran, like a command, a read or an edit.
        case tool
    }

    public var id: String
    public var role: Role
    /// Your words, the agent's Markdown, or a tool's one line, like "Run the tests" or "Farol/SessionView.swift".
    public var text: String
    /// The tool's name, like "Bash", "Read" or "Edit".
    public var tool: String?
    /// What a tool did in full: a command, or an edit as a unified diff.
    public var detail: String?

    public init(id: String, role: Role, text: String, tool: String? = nil, detail: String? = nil) {
        self.id = id
        self.role = role
        self.text = text
        self.tool = tool
        self.detail = detail
    }
}

/// What an agent waits on you for: a tool it asks to run, or a question. From the agent's hooks, so the phone shows exactly what is asked
/// without reading it off the screen.
public struct RemoteRequest: Codable, Equatable, Sendable {
    /// Tells this request from the next one, so an answer to one that is gone is never typed into another.
    public var id: String
    /// The tool asking, like "Bash" or "Edit". Nil for a question.
    public var tool: String?
    /// One line: the command, the file, or the question.
    public var title: String
    /// What it does in full: a script, an edit as a unified diff, or what a question is about.
    public var detail: String?
    /// The answers, in the order the agent numbers them. Empty when the hook does not say, and the phone reads them off the screen.
    public var choices: [Choice]

    public struct Choice: Codable, Equatable, Sendable {
        public var label: String
        public var detail: String?

        public init(label: String, detail: String? = nil) {
            self.label = label
            self.detail = detail
        }
    }

    public init(id: String, tool: String?, title: String, detail: String? = nil, choices: [Choice] = []) {
        self.id = id
        self.tool = tool
        self.title = title
        self.detail = detail
        self.choices = choices
    }
}

/// Something an agent takes after a slash: one of its own, a skill or command of yours or the project's, or a plugin's.
public struct RemoteAgentCommand: Codable, Equatable, Sendable {
    /// Without the slash or dollar sign, like "compact" or "frontend-design:review".
    public var name: String
    public var detail: String?
    /// It opens a picker in the agent's terminal, like /model, so the phone shows the terminal once it is sent.
    public var menu: Bool
    /// Called with a dollar sign rather than a slash, as Codex mentions a skill: `$name`.
    public var mention: Bool

    public init(name: String, detail: String? = nil, menu: Bool = false, mention: Bool = false) {
        self.name = name
        self.detail = detail
        self.menu = menu
        self.mention = mention
    }

    /// What it is typed with, a slash or a dollar sign.
    public var sigil: Character { mention ? "$" : "/" }
}

/// A picture sent from the phone to the host, for an agent to look at. It travels in parts, since a message holds at most 256 KB.
public enum RemoteAttachment {
    /// Small enough that a part stays well under a message once sealed. It is base64 twice on the way, in the command and in the frame
    /// around the sealed command, so a part grows by about four fifths.
    public static let partSize = 100_000
    /// A photo the phone has made smaller is far below this. Anything larger is not what attaching is for.
    public static let maxSize = 10 << 20

    /// The parts to send, in order.
    public static func parts(of data: Data) -> [Data] {
        stride(from: 0, to: data.count, by: partSize).map { data[$0..<min($0 + partSize, data.count)] }.map { Data($0) }
    }
}

/// A file changed in a session's repository.
public struct RemoteChange: Codable, Equatable, Identifiable, Sendable {
    public enum Status: String, Codable, Sendable {
        case modified, added, deleted, renamed
    }

    public var path: String
    /// Where a renamed file was before.
    public var oldPath: String?
    public var status: Status
    public var added: Int
    public var removed: Int
    /// No lines to count or show.
    public var binary: Bool

    public var id: String { path }

    public init(path: String, oldPath: String? = nil, status: Status, added: Int, removed: Int, binary: Bool = false) {
        self.path = path
        self.oldPath = oldPath
        self.status = status
        self.added = added
        self.removed = removed
        self.binary = binary
    }
}

/// What a Live Activity shows. The relay writes it into the push from what the Mac sends, so the names match `relay/push.go`.
/// The counts are all Apple and the relay see. `sealed` is the sessions, from `NoticeBox`, which only the phone opens.
public struct RemoteActivityState: Codable, Hashable, Sendable {
    public var waiting: Int
    public var working: Int
    public var sealed: String?

    public init(waiting: Int, working: Int, sealed: String? = nil) {
        self.waiting = waiting
        self.working = working
        self.sealed = sealed
    }

    /// The counts, and the first few sessions that need you or work, sealed with the phone's key.
    /// Only a few, since Apple caps a push at 4 KB and the relay a sealed part at 2000 characters.
    public init(_ sessions: [RemoteSession], key: Data?) {
        let needsYou = sessions.filter { $0.status == .waiting || $0.status == .stopped }
        let working = sessions.filter { $0.status == .working }
        let shown = (needsYou + working).prefix(4).map { RemoteSession(id: $0.id, name: String($0.name.prefix(60)), branch: nil, status: $0.status) }
        self.init(waiting: needsYou.count, working: working.count,
                  sealed: key.flatMap { NoticeBox.seal(Array(shown), key: $0) }?.base64EncodedString())
    }

    /// Nothing waits and nothing works, so there is nothing left to follow.
    public var isQuiet: Bool { waiting == 0 && working == 0 }

    /// The sessions it names, once opened with the phone's key. Empty when it cannot be opened.
    public func sessions(key: Data?) -> [RemoteSession] {
        guard let key, let sealed, let data = Data(base64Encoded: sealed) else { return [] }
        return NoticeBox.open(data, as: [RemoteSession].self, key: key) ?? []
    }
}

/// A project or an agent a new task can use. The id means something to the Mac only, and the phone hands it back as it came.
public struct RemoteChoice: Codable, Equatable, Identifiable, Hashable, Sendable {
    public var id: String
    public var name: String

    public init(id: String, name: String) {
        self.id = id
        self.name = name
    }
}

/// Something the phone asks of the Mac. The id is its own, so the Mac can tell a command sent twice from two commands.
public struct RemoteCommand: Codable, Equatable, Sendable {
    public var id: UUID
    public var action: Action

    public enum Action: Codable, Equatable, Sendable {
        case hello(version: Int)
        /// The session whose screen the phone shows. Nil when it shows the list, so the Mac reads no pane.
        case watch(session: UUID?)
        /// Goes in whole, as a paste, and waits for a Return.
        case send(session: UUID, text: String)
        case key(session: UUID, key: RemoteKey)
        /// Typed as keys, one after the other, so a program that reacts to each one does: a menu to a digit, an agent to a slash.
        case type(session: UUID, text: String)
        /// Where Apple can reach this phone with a notification. `sandbox` is true for a build run from Xcode.
        /// `notices` is what the phone wants to hear about. An older phone leaves it out, and hears about everything.
        /// `key` lets the Mac seal which session a notification is about, so only this phone can read it.
        /// `starts` is where Apple can start a Live Activity on this phone while the app is closed, from iOS 17.2.
        /// It rides here rather than in a command of its own, so a Mac that does not know it just ignores it.
        case notify(token: Data, sandbox: Bool, notices: [RemoteNotice]? = nil, key: Data? = nil, starts: Data? = nil)
        /// Where Apple can reach the Live Activity this phone started, to keep it current while the app is closed.
        /// A Mac that does not know it refuses it, and the phone goes on without updates.
        case activity(token: Data, sandbox: Bool)
        /// What scrolled off the top of a session's screen. Asked for once, when someone wants to read back.
        case history(session: UUID)
        /// What a new task can be started in and with. Asked for when the phone opens its New task sheet.
        case options
        /// Starts an agent on a task in a new worktree. `project` and `agent` are ids from `options`.
        case task(project: String, agent: String, text: String)
        /// Opens a new session in a project from `options`, like a new tab on the host. It runs the agent with that id from `options`,
        /// or is a plain shell when `agent` is nil. Ids only, so a phone picks from the host's lists and never sends a command to run.
        case open(project: String, agent: String?)
        /// Closes a session, as its tab's close button on the host. A host keeps its last session open, and says so.
        case close(session: UUID)
        /// The files changed in a session's repository, as the host's review panel compares them.
        case changes(session: UUID)
        /// One of those files, as a unified diff.
        case diff(session: UUID, path: String)
        /// The conversation of the agent in a session, read from the agent's own log rather than its screen.
        /// Nil asks for the latest messages, and the host then sends new ones as they come while the session is watched.
        /// A message's id asks for the page just before it, to read further back.
        case conversation(session: UUID, before: String?)
        /// Picks the answer numbered `choice`, from 1, as the agent numbers them. Only while `request` is still the one waiting,
        /// so a tap that arrives late is refused rather than typed into whatever the agent shows next.
        case answer(session: UUID, request: String, choice: Int)
        /// The phone shows the session's terminal, or stopped showing it. While it does, the host sends its screen often.
        /// A phone that never says gets the screen as before, which is how a host tells an older phone apart.
        case terminal(session: UUID, open: Bool)
        /// A letter with Control held, like Control-D to end a shell or Control-R to search its history. `key` is one of a to z.
        case control(session: UUID, key: String)
        /// What the agent in a session takes after a slash. Asked once, when a reply starts with one.
        case commands(session: UUID)
        /// One part of a picture, `part` counting from 0 of `parts`. The host answers each one before the next goes, with `done`,
        /// and the last with `attached`. Sent again after a dropped connection, a part already taken is taken once.
        case attach(session: UUID, attachment: UUID, part: Int, parts: Int, data: Data)
    }

    public init(id: UUID = UUID(), _ action: Action) {
        self.id = id
        self.action = action
    }
}

/// Something the Mac tells the phone.
/// The sessions and the screen are always sent in full, so a phone that missed some has nothing to replay.
public enum RemoteEvent: Codable, Equatable, Sendable {
    case hello(version: Int)
    case sessions([RemoteSession])
    case screen(session: UUID, text: String)
    /// The lines above the screen, oldest first, as far back as fits in one message.
    case history(session: UUID, text: String)
    case options(projects: [RemoteChoice], agents: [RemoteChoice], agent: String?)
    case done(command: UUID)
    /// The session an `open` made, so the phone can go straight to it. It answers that command, as `done` does.
    case opened(command: UUID, session: UUID)
    /// Answers `changes`. `scope` says what they are compared with, like "Uncommitted" or "Since main".
    case changes(session: UUID, scope: String, files: [RemoteChange])
    /// Messages of a session's conversation, and where they go. `more` says there are earlier ones to ask for.
    case conversation(session: UUID, messages: [RemoteMessage], place: RemotePlace, more: Bool)
    /// Answers `diff`: the file as a unified diff, `@@` headers and `+`, `-` and ` ` lines, cut short if very long.
    case diff(session: UUID, path: String, text: String)
    /// What the watched session waits on you for, sent when it changes. Nil once it no longer waits.
    case request(session: UUID, request: RemoteRequest?)
    /// Answers the last part of `attach`: where the host put the picture, to put in a reply. The host names the file, never the phone.
    case attached(command: UUID, attachment: UUID, path: String)
    /// Answers `commands`, sorted by name.
    case commands(session: UUID, commands: [RemoteAgentCommand])
    case refused(command: UUID, reason: String)
}

/// The commands already carried out, so one that arrives twice is typed once.
/// A phone resends after a dropped connection, and only the last few can still be in doubt.
public struct SeenCommands {
    private var order: [UUID] = []
    private var seen: Set<UUID> = []
    private let limit: Int

    public init(limit: Int = 256) {
        self.limit = limit
    }

    /// True the first time an id is seen.
    public mutating func insert(_ id: UUID) -> Bool {
        guard seen.insert(id).inserted else { return false }
        order.append(id)
        if order.count > limit { seen.remove(order.removeFirst()) }
        return true
    }
}
