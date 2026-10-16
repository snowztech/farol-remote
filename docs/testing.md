# Testing Remote end to end

What to try with a real Mac, a real iPhone and a real agent before Remote goes to testers. Each step says what should happen. Note anything else, with the time, so it can be found in the Mac's log.

## Set up

1. Farol on the Mac from source, against your copy of this package: in `farol`, run `swift package edit farol-remote --path ../farol-remote`, then `make run`. Run `swift package unedit farol-remote` and `git checkout Package.resolved` when you are done.
2. Farol for iPhone from Xcode on your phone, with your copy of this package dragged into the project.
3. In Settings → Remote on the Mac, turn Remote on. If a row asks to update or connect Claude Code, do it, then start Claude Code again so it asks to approve the new hooks once.
4. Pair the phone with the code.

## The list and new sessions

- [ ] The list groups sessions under Needs you, Working, and Done and idle. A session moves between them within a second of its dot changing on the Mac.
- [ ] New session starts Claude Code in the project you pick. The phone opens it on "Nothing said yet", not on the terminal.
- [ ] Your first message there reaches the agent, shows as Sending then Sent, and the conversation fills in as the agent answers.
- [ ] "Open a plain shell instead" opens a shell, straight on its terminal.

## Answering

Ask the agent for something that needs permission, like "run `ls` in a new folder you create".

- [ ] A Bash approval shows "Needs your approval", the command, and the answers as buttons, numbered as on the Mac.
- [ ] Tapping Yes runs it. The card says "Sending your answer…" until the agent moves on.
- [ ] Tap an answer twice fast. Only one is typed on the Mac.
- [ ] Answer on the Mac while the card is on the phone, then tap on the phone. The phone's tap is refused and nothing is typed.
- [ ] An Edit approval shows the file and the start of the change.
- [ ] A question from AskUserQuestion shows "Asks you", the question, and each answer with its description. Picking one answers it.
- [ ] A question with several parts falls back to the options read off the screen, and still answers.
- [ ] With Codex, an approval shows and answers from the options read off the screen.

## Replies

- [ ] A reply while the agent works shows as Queued, then joins the conversation once the agent reads it.
- [ ] A long paste of 20 lines folds to 8 with Show all, and shows once in the conversation, not twice.
- [ ] Turn on airplane mode, send a reply: it shows "Sends when your Mac is back" with a dashed outline. Turn airplane mode off: it goes, once.
- [ ] `/clear` in Claude Code, from the phone: the conversation starts over.
- [ ] Stop, and Interrupt from the ⋯ menu, stop the agent.

## Pictures

- [ ] + in the reply picks a screenshot. Its thumbnail fills a ring as it goes, and Send waits for it.
- [ ] Sent with a question, the agent looks at the picture and answers about it, in Claude Code and in Codex.
- [ ] Your message shows "Picture" above your words, not a path.
- [ ] Four photos at once all arrive. A large photo from the camera arrives too, made smaller on the way.
- [ ] Turn on airplane mode while a picture is on its way, then off: it finishes, once.
- [ ] The file is in ~/Library/Caches/Farol/Attachments on the Mac, and only you can read it.

## Terminal

- [ ] The terminal icon opens the terminal as a screen. Typing `ls` there shows its output within a moment.
- [ ] Output that runs, like `ping -c 5 1.1.1.1`, appears line by line, and the screen follows it unless you scroll up.
- [ ] ctrl → ⌃C stops it. ⌃D ends a shell. ⌃R searches its history.
- [ ] Show earlier output brings back what scrolled off. In Claude Code it says there is nothing earlier.
- [ ] Going back to the conversation and into the terminal again keeps the conversation where it was.

## Notifications and the lock screen

- [ ] With the app closed, a waiting agent notifies, and tapping the notification opens that session.
- [ ] The Live Activity counts what waits and works, and ends when nothing does.

## Across versions

- [ ] The new iPhone app with the released Farol on the Mac: sessions, conversations and answers work. The terminal says to update Farol on the Mac, and has no ctrl key.
- [ ] The released iPhone app with the new Farol on the Mac: sessions, the screen and replies work.
- [ ] The new iPhone app with `farol-host` on a server: its sessions open on their terminal, and replies work.

## Connection

- [ ] Put the Mac to sleep and wake it: the phone reconnects by itself within a few seconds, on the same session.
- [ ] Switch the phone from Wi-Fi to cellular mid-session: it reconnects, and nothing sent is lost or sent twice.
- [ ] Quit Farol on the Mac: the phone says the Mac is not connected. Open it again: the phone comes back.
