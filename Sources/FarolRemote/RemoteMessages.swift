import Foundation

/// What a paired phone and the Mac say to each other. Shared with the iOS app, so nothing here knows of git or AppKit.
public enum RemoteProtocol {
    /// Raised when a message changes meaning. Both sides say theirs first, and only the same number talks.
    public static let version = 1

    public static func accepts(_ other: Int) -> Bool { other == version }
}

/// A key that isn't text, for a prompt that waits on Return or an arrow.
public enum RemoteKey: String, Codable, CaseIterable, Sendable {
    case enter, escape, tab, up, down, left, right, backspace
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
