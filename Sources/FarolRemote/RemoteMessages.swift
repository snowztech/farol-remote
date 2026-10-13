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
        case notify(token: Data, sandbox: Bool, notices: [RemoteNotice]? = nil, key: Data? = nil)
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
