import FarolRemote
import Foundation

// A phone in a terminal: `RemoteProbe <pairing link>` pairs as a scan would.
// Then it takes `watch <n>`, `send <text>`, `key <name>` and `quit` on standard input.

final class Probe {
    private let phone = DeviceKey()
    private let offer: PairingOffer
    private var link: PhoneLink
    private var pairing: PairingRequest?
    private var sessions: [RemoteSession] = []
    private var watched: UUID?
    private let connection: RelayConnection

    init(offer: PairingOffer) throws {
        self.offer = offer
        link = PhoneLink(phone: phone, mac: offer.mac)
        pairing = try Pairing.request(for: offer, from: phone, named: "RemoteProbe")
        connection = RelayConnection(url: URL(string: "\(offer.relay)/v1/rooms/\(RelayFrame.room(for: offer.mac))/phone")!)
        connection.onState = { print($0 ? "# relay reached" : "# relay lost") }
        connection.onMessage = { [unowned self] in receive($0) }
        connection.start()
    }

    private func send(_ frames: [WireFrame]) {
        for frame in frames {
            guard let payload = try? JSONEncoder().encode(frame) else { continue }
            connection.send(RelayFrame.encode(payload))
        }
    }

    private func ask(_ action: RemoteCommand.Action) {
        guard let frame = link.seal(RemoteCommand(action)) else { return print("! not connected yet") }
        send([frame])
    }

    private func receive(_ message: Data) {
        switch RelayFrame(message, readBy: .phone) {
        case .online:
            print("# mac is here")
            link = PhoneLink(phone: phone, mac: offer.mac)
            send(link.start(pairing: pairing))
        case .offline:
            print("# mac is not connected")
        case .data(_, let payload):
            guard let frame = try? JSONDecoder().decode(WireFrame.self, from: payload) else { return }
            link.receive(frame).forEach(show)
        default:
            break
        }
    }

    private func show(_ output: PhoneLink.Output) {
        switch output {
        case .send(let frame): send([frame])
        case .paired:
            print("# paired")
            pairing = nil
        case .ready:
            print("# encrypted")
            ask(.hello(version: RemoteProtocol.version))
        case .event(.hello(let version)): print("# mac speaks version \(version)")
        case .event(.sessions(let list)):
            sessions = list
            print("# sessions")
            for (place, session) in list.enumerated() {
                print("  \(place + 1)  \(session.status.rawValue)  \(session.name)  \(session.branch ?? "")")
            }
        case .event(.screen(_, let text)): print("# screen\n\(text)\n# end")
        case .event(.history(_, let text)): print("# history\n\(text)\n# end")
        case .event(.options(let projects, let agents, _)): print("# projects \(projects.map(\.name)) agents \(agents.map(\.name))")
        case .event(.done): print("# done")
        case .event(.opened(_, let session)): print("# opened \(session)")
        case .event(.refused(_, let why)): print("! refused: \(why)")
        case .refused(let why): print("! \(why)")
        case .close: print("! connection closed")
        }
    }

    func type(_ line: String) {
        let words = line.split(separator: " ", maxSplits: 1).map(String.init)
        switch (words.first, words.count > 1 ? words[1] : nil) {
        case ("watch", let place?):
            guard let place = Int(place), sessions.indices.contains(place - 1) else { return print("! no such session") }
            watched = sessions[place - 1].id
            ask(.watch(session: watched))
        case ("send", let text?):
            watched.map { ask(.send(session: $0, text: text)) }
        case ("key", let name?):
            guard let key = RemoteKey(rawValue: name), let watched else { return print("! watch a session first, then a key name") }
            ask(.key(session: watched, key: key))
        case ("quit", _):
            exit(0)
        default:
            print("! watch <n> | send <text> | key <name> | quit")
        }
    }
}

guard CommandLine.arguments.count == 2, let link = URL(string: CommandLine.arguments[1]),
      let offer = PairingOffer(link: link) else {
    print("usage: RemoteProbe <pairing link>")
    exit(64)
}
setvbuf(stdout, nil, _IOLBF, 0)
let probe = try Probe(offer: offer)
// Lines are read here and handed to the main queue, where the connection also answers.
FileHandle.standardInput.readabilityHandler = { input in
    let text = String(decoding: input.availableData, as: UTF8.self)
    DispatchQueue.main.async {
        text.split(separator: "\n").forEach { probe.type(String($0)) }
    }
}
RunLoop.main.run()
