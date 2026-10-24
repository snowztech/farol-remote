import Foundation

/// What an agent asks on its screen: a numbered menu and the words over it.
/// Each agent frames it its own way, Claude Code between rules, Codex under a gap, opencode behind a bar on its left,
/// so it is read from the menu up, and only while the menu is the last thing on the screen. A numbered list the agent wrote,
/// with its prompt under it, is not a question.
public struct Prompt: Equatable, Sendable {
    public struct Option: Equatable, Sendable {
        public var number: Int
        public var label: String
        /// What the agent says under it, like a question option's description.
        public var detail: String?
        /// The key the agent names for it, like "y" or "esc", taken off the end of the label.
        public var key: String?
    }

    /// The question, like "Do you want to proceed?" or "Would you like to run the following command?".
    public var title: String
    /// What it is about, like Codex's reason or Claude Code's tool and its description.
    public var note: String?
    /// The command it would run, as Codex shows it after a `$`.
    public var command: String?
    public var options: [Option]
    /// The option the agent's cursor is on, which Return picks.
    public var selected: Int?
    /// The option that takes your own words, like Claude Code's "Type something" or Codex's "Other".
    public var free: Int?
    /// The tabs of a form with several questions, as Claude Code draws them.
    public var steps: [Screen.Step]

    public init?(screen: String) {
        let rows = Self.rows(of: screen)
        let width = rows.map(\.count).max() ?? 0
        guard let last = rows.lastIndex(where: { Self.option(in: $0) != nil }) else { return nil }

        // Up from the last option to the first, which is 1. Rows between are what an option says under it.
        var first = last
        var number = Self.option(in: rows[last])!.number
        var row = last - 1
        while number > 1, row >= 0, last - row < 40 {
            if let option = Self.option(in: rows[row]) {
                guard option.number == number - 1 else { return nil }
                number -= 1
                first = row
            }
            row -= 1
        }
        guard number == 1, first < last else { return nil }

        // Down the menu: each option, its wrapped words, and its description.
        var options: [Option] = []
        var selected: Int?
        var column = 0
        var previous = 0
        var end = last
        for index in first..<rows.count {
            let text = rows[index]
            if let option = Self.option(in: text) {
                options.append(Option(number: option.number, label: option.label))
                if option.marked { selected = option.number }
                column = option.column
                previous = text.count
                end = index
            } else if Self.isBlank(text) || Self.isRule(text) {
                previous = 0
                // Past the last option, only the keys the menu takes may follow.
                if index > last { break }
            } else if index > last, Self.isHint(text) {
                // Like the Submit under a form's last question.
                break
            } else if text.trimmingCharacters(in: .whitespaces) == "Submit" {
                // The row under a form's last question that sends it, which is the form's, not an option's.
                previous = 0
            } else if Self.indent(of: text) >= column, previous > 0 || index <= last {
                let words = text.trimmingCharacters(in: .whitespaces)
                if previous >= width - 16, options[options.count - 1].detail == nil {
                    options[options.count - 1].label = Self.join(options[options.count - 1].label, words, broken: previous == width)
                } else {
                    options[options.count - 1].detail = options[options.count - 1].detail.map { $0 + " " + words } ?? words
                }
                previous = text.count
                end = index
            } else if index > last {
                break
            } else {
                return nil
            }
        }
        let after = rows[(end + 1)...]
        guard after.allSatisfy({ Self.isBlank($0) || Self.isRule($0) || Self.isHint($0) }) else { return nil }
        // A menu shows where its cursor is or which keys it takes. A list with neither is only text.
        guard selected != nil || after.contains(where: Self.isHint) else { return nil }

        for index in options.indices {
            if let match = options[index].label.firstMatch(of: #/\s\(([a-z]|esc)\)$/#) {
                options[index].key = String(match.1)
                options[index].label = String(options[index].label[..<match.range.lowerBound])
            }
        }
        self.options = options
        self.selected = selected
        free = options.first { option in
            let label = option.label.lowercased()
            return label.hasPrefix("type something") || label.hasPrefix("type your own") || label == "other"
        }?.number

        // Up from the menu to where the question starts: a rule, the agent's last message, or a wide gap.
        var top = first
        var blanks = 0
        row = first - 1
        while row >= 0, first - row <= 16 {
            let text = rows[row]
            if Self.isBlank(text) {
                blanks += 1
                if blanks >= 3 { break }
            } else {
                // Claude Code sets a command between dashed rules inside its question.
                if Self.isRule(text) && !Self.isDashed(text) || Self.isBoundary(text) { break }
                blanks = 0
                top = row
            }
            row -= 1
        }
        let body = rows[top..<first]
        steps = body.lazy.map { Screen.steps(in: String($0)) }.first { !$0.isEmpty } ?? []
        let words = Self.words(body.filter { Screen.steps(in: String($0)).isEmpty }, width: width)
        title = words.title
        note = words.note
        command = words.command
    }

    /// What a screen asks, in one line for a list: the command its menu would run, or its question, or how many questions Codex holds.
    /// Nil when the screen asks nothing.
    public static func asking(in screen: String) -> String? {
        if let prompt = Prompt(screen: screen) {
            return prompt.command.flatMap { $0.split(separator: "\n").first }.map { "Run \($0)?" } ?? (prompt.title.isEmpty ? nil : prompt.title)
        }
        return queued(in: screen).map { $0 == 1 ? "A question" : "\($0) questions" }
    }

    /// Codex keeps the questions it asks while it works behind Shift-Left: "? 2 questions · 7s" over "⇧← to answer".
    public static func queued(in screen: String) -> Int? {
        let rows = screen.split(separator: "\n").suffix(24)
        guard rows.contains(where: { $0.contains("to answer") }) else { return nil }
        return rows.lazy.compactMap { $0.firstMatch(of: #/^\s*\?\s+(\d+)\s+questions?\b/#).flatMap { Int($0.1) } }.first
    }

    // MARK: Rows

    /// The screen's rows without the bars opencode and boxes draw at their sides, or the spaces after them.
    private static func rows(of screen: String) -> [Substring] {
        screen.split(separator: "\n", omittingEmptySubsequences: false).map { row in
            var row = row
            while let last = row.last, last.isWhitespace || "│┃".contains(last) && row.dropLast().last?.isWhitespace == true { row = row.dropLast() }
            if let bar = row.firstIndex(where: { !$0.isWhitespace }), "│┃".contains(row[bar]) { row = row[row.index(after: bar)...] }
            return row
        }
    }

    private static func option(in row: Substring) -> (number: Int, label: String, marked: Bool, column: Int)? {
        guard let match = row.wholeMatch(of: #/(\s*)([❯›>]\s*)?(\d{1,2})\.\s+(.+)/#), let number = Int(match.3) else { return nil }
        // A multiple choice draws a box before each label, which the phone shows its own way.
        let label = match.4.replacing(#/^\[[ xX✔✓]\]\s*/#, with: "")
        return (number, String(label), match.2 != nil, row[..<match.4.startIndex].count)
    }

    private static func isBlank(_ row: Substring) -> Bool { row.allSatisfy(\.isWhitespace) }

    /// A row of dashes or a box's edge, drawn across the terminal.
    private static func isRule(_ row: Substring) -> Bool {
        let drawn = row.filter { !$0.isWhitespace }
        return drawn.count >= 8 && drawn.unicodeScalars.allSatisfy { (0x2500...0x259F).contains($0.value) || "-=".unicodeScalars.contains($0) }
    }

    private static func isDashed(_ row: Substring) -> Bool {
        isRule(row) && row.contains { "╌┄┈╍┅┉".contains($0) }
    }

    /// The keys a menu takes, like "Press enter to confirm or esc to cancel" or "↑↓ select  enter submit".
    private static func isHint(_ row: Substring) -> Bool {
        row.firstMatch(of: #/(?i)\b(enter|esc|return|submit)\b|↑|↓|\bto (select|navigate|cancel|confirm)\b/#) != nil
    }

    /// Where what an agent wrote ends: its bullets, Claude Code's ⏺ and ⎿, or your own prompt echoed back.
    private static func isBoundary(_ row: Substring) -> Bool {
        let text = row.drop { $0.isWhitespace }
        guard let first = text.first else { return false }
        return "•⏺●■✗⎿└╭╰".contains(first) || text.hasPrefix("› ") || text.hasPrefix("> ")
    }

    private static func indent(of row: Substring) -> Int { row.prefix { $0.isWhitespace }.count }

    /// A line the terminal wrapped goes on with a space, or without one where it broke a word: at a hyphen, or at the screen's edge.
    private static func join(_ text: String, _ more: String, broken: Bool = false) -> String {
        broken || text.hasSuffix("-") ? text + more : text + " " + more
    }

    /// The question, what it is about, and the command it would run, from the rows over the menu.
    /// A row nearly as wide as the screen goes on in the next, which the terminal wrapped. Any other row ends a line.
    private static func words(_ rows: [Substring], width: Int) -> (title: String, note: String?, command: String?) {
        var lines: [(text: String, wide: Bool, full: Bool)] = []
        var command: [String] = []
        var inCommand = false
        for row in rows {
            let text = row.trimmingCharacters(in: .whitespaces)
            if isDashed(row) {
                inCommand.toggle()
                continue
            }
            if text.hasPrefix("$ ") { inCommand = true }
            if text.isEmpty { inCommand = false }
            // Claude Code's tip, and the tab of a form's only question.
            if text.hasPrefix("Tip: ") || text.first.map({ "□☐☒☑←".contains($0) }) == true { continue }
            if inCommand {
                command.append(text.hasPrefix("$ ") ? String(text.dropFirst(2)) : text)
            } else if text != "Environment: local" {
                lines.append((text, row.count >= width - 16, row.count == width))
            }
        }
        // Soft wraps joined, so each line is one sentence or one paragraph.
        var joined: [String] = []
        var wraps = false, broken = false
        for line in lines {
            if wraps, !line.text.isEmpty, let last = joined.popLast() { joined.append(join(last, line.text, broken: broken)) } else { joined.append(line.text) }
            wraps = line.wide
            broken = line.full
        }
        let titleIndex = joined.firstIndex { $0.hasSuffix("?") } ?? joined.firstIndex { !$0.isEmpty }
        let title = titleIndex.map { joined[$0] } ?? ""
        let note = joined.indices.filter { $0 != titleIndex }.map { joined[$0] }
            .map { $0.hasPrefix("Reason: ") ? String($0.dropFirst(8)) : $0 }
            .split(separator: "", omittingEmptySubsequences: true).map { $0.joined(separator: "\n") }.joined(separator: "\n\n")
        return (title, note.isEmpty ? nil : note, command.isEmpty ? nil : command.joined(separator: "\n"))
    }
}
