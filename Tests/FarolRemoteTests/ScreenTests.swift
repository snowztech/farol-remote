import Testing
@testable import FarolRemote

private let rule = String(repeating: "─", count: 40)

@Test func readsMultipleQuestionSteps() {
    let steps = Screen.steps(in: "intro\n← ☒ Scope ☐ Approach ✔ Submit →\nWhich approach should I take?")
    #expect(steps.map(\.label) == ["Scope", "Approach", "Submit"])
    #expect(steps.map(\.done) == [true, false, true])
}

@Test func readsTheSpinnerLine() {
    let screen = "⏺ Reading src/main.ts\n\n✻ Cogitating… (42s · ↑ 1.2k tokens · esc to interrupt)\n\n" + rule + "\n> \n" + rule
    #expect(Screen.status(in: screen)?.word == "Cogitating…")
    #expect(Screen.status(in: screen)?.detail == "42s · ↑ 1.2k tokens")
    #expect(Screen.status(in: "✢ Compacting conversation…")?.word == "Compacting conversation…")
    #expect(Screen.status(in: "✢ Compacting conversation…")?.detail == "")
}

@Test func aDoneOrQuietScreenHasNoStatus() {
    #expect(Screen.status(in: "⏺ Done.\n\n✻ Worked for 38s") == nil)
    #expect(Screen.status(in: "⏺ Thinking… about it") == nil)
}

@Test func readsCodexsWorkingLine() {
    // As Codex 0.163.0 draws it, under one of its own bulleted messages.
    let screen = "• I’ll count the lines in notes.txt.\n• Ran wc -l notes.txt\n\n• Working (10s • esc to interrupt)\n\n› Ask Codex to do anything"
    #expect(Screen.status(in: screen)?.word == "Working")
    #expect(Screen.status(in: screen)?.detail == "10s")
    let starting = "• Starting MCP servers (5/6): openseo (1s • esc to interrupt)"
    #expect(Screen.status(in: starting)?.word == "Starting MCP servers (5/6): openseo")
    #expect(Screen.status(in: "• notes.txt contains 2 lines.\n  Worked for 12s • 19:28\n› Ask Codex to do anything") == nil)
}
