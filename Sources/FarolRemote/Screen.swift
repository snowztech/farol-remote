import Foundation

/// What is on a terminal's screen.
public enum Screen {
    /// The tab row Claude Code draws while it walks through several questions and a final review.
    public struct Step: Equatable, Sendable {
        public var label: String
        public var done: Bool
    }

    /// From the first row that has them.
    public static func steps(in screen: String) -> [Step] {
        for row in screen.split(separator: "\n") {
            let parts = row.matches(of: #/([□☐☒☑✔✓])\s*([^□☐☒☑✔✓←→]+)/#)
            guard parts.count >= 2 else { continue }
            return parts.map { Step(label: String($0.2).trimmingCharacters(in: .whitespaces), done: "☒☑✔✓".contains($0.1)) }
        }
        return []
    }

    /// What the agent says it is doing on its spinner line, as "Cogitating…" and "42s · ↑ 1.2k tokens".
    /// Claude Code draws `✻ Cogitating… (42s · ↑ 1.2k tokens · esc to interrupt)`, Codex `• Working (10s • esc to interrupt)`.
    /// It moves while the agent writes, which its log only has once it is done.
    /// Nil without a spinner near the end, as when it says it worked for a while.
    public static func status(in screen: String) -> (word: String, detail: String)? {
        for row in screen.split(separator: "\n").suffix(16).reversed() {
            let claude = row.firstMatch(of: #/^\s*[·✢✳✶✻✽*]\s+([^(…]+…)\s*(?:\((.*)\))?/#).map { ($0.1, $0.2) }
            // Codex starts its messages with a bullet too, so only the line that says how to interrupt counts.
            let codex = row.firstMatch(of: #/^\s*•\s+(.+?)\s+\(([^()]*esc to interrupt)\)\s*$/#).map { ($0.1, Optional($0.2)) }
            guard let (word, inside) = claude ?? codex else { continue }
            let keys = (inside ?? "").components(separatedBy: " · ").flatMap { $0.components(separatedBy: " • ") }
                .filter { !$0.contains("esc to") && !$0.contains("ctrl+") }
            return (String(word), keys.joined(separator: " · "))
        }
        return nil
    }
}
