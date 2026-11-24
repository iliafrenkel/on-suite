# AI assistants

You can connect an AI assistant, such as Claude, ChatGPT or Gemini, to
your ON Suite. Once connected, you can ask it about your own things — for
example "what should I read next?" — and it looks in ON Suite to answer.

Today an assistant can read your **ON Books** reading log: your shelves,
ratings, reviews, notes, quotes and reading stats. If you allow it, it
can also make changes there: start a book, record your progress, finish
a book (with a rating), mark one as not finished, and add notes and
quotes. It can't delete anything.

An assistant can also read your **ON Notes**: it can search your notes,
read a list or a project, and tell you what's due. If you allow it, it
can add a new bullet and tick one off as done. It can't delete, move or
edit your bullets. More apps will follow.

For example, you can say "I'm on page 120 of Dune", "I finished Dune,
five stars", "save this quote from Dune: ...", "add milk to my groceries
list", "what's due this week?" or "tick off call Sam".

An assistant only ever sees **your** things, never anyone else's in your
household, and only the parts you allow when you connect it.

## Before you start: the admin turns it on

AI access is off until the person who runs your ON Suite turns it on. If
you don't see **Connected assistants** on your **Account** page, it's off;
ask your admin. The [Admin guide](admin.md#ai-access) explains the three
settings it needs.

Your ON Suite must also be reachable from the internet at an `https://`
address, because the assistant's company connects to it from their
servers.

## Connecting an assistant

Each assistant has its own settings screen for this, and the names of the
buttons change from time to time, but the steps are the same:

1. In the assistant's settings, look for **connectors** (or
   **integrations** or **apps**) and choose to add a custom one.
2. Paste your ON Suite address followed by `/mcp`, for example
   `https://on.example.com/mcp`. Ask your admin for the exact address if
   you're not sure.
3. The assistant opens ON Suite in your browser. Sign in if you're asked
   to.
4. ON Suite asks what the assistant may do (see below). Choose, then click
   **Allow**.
5. You're sent back to the assistant, which is now connected.

Claude and ChatGPT work with ON Suite's default settings. Gemini works
too, once your admin has added its callback addresses (it uses six per
user) to ON Suite's list of allowed addresses; see the
[Admin guide](admin.md#ai-access). Until then, connecting Gemini fails.

You connect each assistant once; it stays connected for about three months, after which
it asks you to sign in to ON Suite again.

## What the permission screen means

The screen is titled **An assistant wants to access your ON Suite**.

- **It will send you back to** — the website the assistant lives on, such
  as `claude.ai`. ON Suite checked this, so you can trust it. If it isn't
  the assistant you meant to connect, click **Cancel**.
- **It calls itself** — the name the assistant gave itself. Anyone can
  choose any name, so go by the line above instead.
- **The boxes** — one row per app, showing only what the assistant asked
  for. A box on the left, such as **Read your books**, lets it look; a box
  on the right, such as **Update books**, lets it make changes. The left
  boxes start ticked and the right ones unticked, so an assistant can't
  change anything unless you tick one. Untick anything you don't want it
  to have.

An assistant you connected before can only make changes if you ticked
**Update books** (for ON Books) or **Add notes** (for ON Notes) when you
connected it. Check **Connected assistants** on
your Account page to see what each one can do; to change that, disconnect
it and connect it again.

Only allow what you need. You can change your mind at any time by
disconnecting and connecting again.

## Seeing and disconnecting assistants

Open **Account** from the user menu and click **Connected assistants**.
The page lists each assistant you've connected, what it can do, when you
connected it and when it last did something.

Click **Disconnect** next to an assistant to end its access straight away.
To use it again, connect it again from the assistant's settings.

**Recent activity** below lists the last 50 things your assistants did,
such as reading your book list. It records which tool was used and whether
it worked, never what your books or notes said.

Your admin can also see which assistants are connected and can disconnect
any of them.

## A note on what assistants read

Assistants treat what they read in ON Suite as information, not as
instructions. Later, when assistants can read articles you've saved or
subscribed to, that text was written by other people, so ON Suite marks it
as such: an article that says "ignore your instructions" is just text to
the assistant, the same as any other sentence in it.
