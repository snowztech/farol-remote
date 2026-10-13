import Foundation

/// A WebSocket to the relay that comes back by itself when the network drops. Everything is delivered on the main queue.
public final class RelayConnection: NSObject, URLSessionWebSocketDelegate {
    public var onMessage: ((Data) -> Void)?
    /// True once the relay answered, false when the connection is lost.
    public var onState: ((Bool) -> Void)?

    private let url: URL
    private lazy var session = URLSession(configuration: .ephemeral, delegate: self, delegateQueue: .main)
    private var task: URLSessionWebSocketTask?
    private var running = false
    private var wait: TimeInterval = 1

    public init(url: URL) {
        self.url = url
    }

    public func start() {
        guard !running else { return }
        running = true
        connect()
    }

    public func stop() {
        running = false
        task?.cancel(with: .goingAway, reason: nil)
        task = nil
    }

    public func send(_ message: Data) {
        task?.send(.data(message)) { _ in }
    }

    private func connect() {
        guard running else { return }
        let task = session.webSocketTask(with: url)
        // The relay's own limit, so a message it would pass is never refused here.
        task.maximumMessageSize = 256 << 10
        self.task = task
        task.resume()
        receive(on: task)
    }

    private func receive(on task: URLSessionWebSocketTask) {
        task.receive { [weak self] result in
            DispatchQueue.main.async {
                guard let self, self.task === task else { return }
                switch result {
                case .success(.data(let message)):
                    self.onMessage?(message)
                    self.receive(on: task)
                case .success:
                    self.receive(on: task)
                case .failure:
                    self.lost()
                }
            }
        }
    }

    private func lost() {
        guard task != nil else { return }
        task = nil
        onState?(false)
        guard running else { return }
        // Waits longer after each failure, so a Mac without network does not spin.
        DispatchQueue.main.asyncAfter(deadline: .now() + wait) { [weak self] in self?.connect() }
        wait = min(wait * 2, 30)
    }

    public func urlSession(_ session: URLSession, webSocketTask: URLSessionWebSocketTask, didOpenWithProtocol protocol: String?) {
        guard webSocketTask === task else { return }
        wait = 1
        onState?(true)
    }
}
