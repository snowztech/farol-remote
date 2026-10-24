import Testing
@testable import FarolRemote

@Test func readsCodexAskingToRunACommand() throws {
    let prompt = try #require(Prompt(screen: Screens.codexApproval))
    #expect(prompt.title == "Would you like to run the following command?")
    #expect(prompt.note == "Allow this command to create the requested file and print ok?")
    #expect(prompt.command == "touch /Users/lucas/farol-probe-delete-me.txt && echo ok")
    #expect(prompt.options.map(\.number) == [1, 2, 3])
    // Wrapped by the terminal, and joined again where it broke the path.
    #expect(prompt.options[1].label == "Yes, and don't ask again for commands that start with `touch /Users/lucas/farol-probe-delete-me.txt`")
    #expect(prompt.options.map(\.key) == ["y", "p", "esc"])
    #expect(prompt.options.allSatisfy { $0.detail == nil })
    #expect(prompt.selected == 1)
    #expect(prompt.free == nil)
}

@Test func readsCodexsQuestionOnceOpened() throws {
    let prompt = try #require(Prompt(screen: Screens.codexQuestion))
    #expect(prompt.title == "Which color?")
    #expect(prompt.note == nil)
    #expect(prompt.options.map(\.label) == ["Red", "Blue", "Other"])
    #expect(prompt.free == 3)
}

@Test func countsTheQuestionsCodexHoldsWhileItWorks() {
    #expect(Prompt.queued(in: Screens.codexQueued) == 1)
    #expect(Prompt(screen: Screens.codexQueued) == nil, "its answers are not on the screen until Shift-Left")
    #expect(Prompt.queued(in: Screens.codexApproval) == nil)
    #expect(Prompt.queued(in: "? 2 questions · 7s\n  ⇧← to answer") == 2)
}

@Test func readsClaudeCodeAskingToRunACommand() throws {
    let prompt = try #require(Prompt(screen: Screens.claudePermission))
    #expect(prompt.title == "Do you want to proceed?")
    #expect(prompt.note == "Bash command\nCreate empty probe.txt file")
    #expect(prompt.command == "touch probe.txt")
    #expect(prompt.options.map(\.label).first == "Yes")
    #expect(prompt.options.map(\.label).last == "No")
    // Broken at the screen's edge in the middle of a word, so joined without a space.
    #expect(prompt.options[1].label == "Yes, and always allow access to /private/tmp/claude-501/-Users-lucas-dev-oss-farol-ios/f29efba3-821f-4032-8bb5-66dbb8e0120c/scratchpad/play from this project")
    #expect(prompt.selected == 1)
}

@Test func readsClaudeCodesFormOneQuestionAtATime() throws {
    let first = try #require(Prompt(screen: Screens.claudeQuestion))
    #expect(first.title == "Which fruit?")
    #expect(first.steps.map(\.label) == ["Fruit", "Colors", "Submit"])
    #expect(first.options.map(\.label) == ["Apple", "Pear", "Type something.", "Chat about this"])
    #expect(first.options[0].detail == "Crisp, sweet or tart.")
    #expect(first.free == 3)

    let second = try #require(Prompt(screen: Screens.claudeMultiple))
    #expect(second.title == "Which colors?")
    #expect(second.steps.map(\.done) == [true, false, true])
    #expect(second.options.map(\.label) == ["Red", "Blue", "Green", "Type something", "Chat about this"])
    #expect(second.options[3].detail == nil, "the Submit row under it is the form's, not the option's")
}

@Test func readsOpencodesQuestionBehindItsBar() throws {
    let prompt = try #require(Prompt(screen: Screens.opencodeQuestion))
    #expect(prompt.title == "Which fruit?")
    #expect(prompt.options.map(\.label) == ["Apple", "Pear", "Type your own answer"])
    #expect(prompt.options[1].detail == "A sweet, juicy fruit")
    #expect(prompt.free == 3)
}

@Test func anAgentWaitingForItsPromptAsksNothing() {
    #expect(Prompt(screen: Screens.claudeIdle) == nil)
    #expect(Prompt(screen: Screens.piIdle) == nil)
    // Its menu has no numbers, so the terminal answers it.
    #expect(Prompt(screen: Screens.claudeTrust) == nil)
}

@Test func aNumberedListTheAgentWroteIsNotAQuestion() {
    let codex = "• Two ways to go:\n\n  1. Keep the cache\n  2. Drop it\n\n› Ask Codex to do anything\n\n  GPT-6-Sol medium · ~/dev/farol"
    #expect(Prompt(screen: codex) == nil)
    let claude = "⏺ Two ways to go:\n  1. Keep the cache\n  2. Drop it\n\n" + String(repeating: "─", count: 40) + "\n❯ \n" + String(repeating: "─", count: 40) + "\n  ? for shortcuts"
    #expect(Prompt(screen: claude) == nil)
    #expect(Prompt(screen: "Pick a database\n1. SQLite\n2. Postgres") == nil, "no cursor and no keys, so only text")
    #expect(Prompt(screen: "1. Only one\n› Ask Codex") == nil)
    #expect(Prompt(screen: "Where?\n1. Left\n   Center the window\n2. Right\n› Ask Codex") == nil)
}

@Test func aFormsOnlyTabIsNotPartOfItsQuestion() throws {
    let prompt = try #require(Prompt(screen: "────────\n ☐ Next step\n\nWhat now?\n\n❯ 1. Run the tests\n  2. Commit\n────────"))
    #expect(prompt.title == "What now?")
    #expect(prompt.note == nil)
}

@Test func aQuestionWithWordsLikeKeysKeepsItsTitle() throws {
    let prompt = try #require(Prompt(screen: "Should I submit the form?\n› 1. Yes\n  2. No\n\nPress enter to confirm or esc to cancel"))
    #expect(prompt.title == "Should I submit the form?")
}

@Test func saysWhatAScreenAsksInOneLine() {
    #expect(Prompt.asking(in: Screens.codexApproval) == "Run touch /Users/lucas/farol-probe-delete-me.txt && echo ok?")
    #expect(Prompt.asking(in: Screens.claudePermission) == "Run touch probe.txt?")
    #expect(Prompt.asking(in: Screens.codexQuestion) == "Which color?")
    #expect(Prompt.asking(in: Screens.codexQueued) == "A question")
    #expect(Prompt.asking(in: Screens.claudeIdle) == nil)
}
