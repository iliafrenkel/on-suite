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
read a list or a project, and tell you what's due. If you tick **Add
notes**, it can also add a new bullet, tick one done (or un-tick it),
and set, change or clear a due date. It can't delete, move or edit your
bullets, and it never sees archived bullets.

It can read your **ON Reader** too: it can list what's new in your
feeds, search them, and read or summarise an article. If you tick **Mark
read / star**, it can also mark articles read (or unread) and star them.
It can't add, remove or change your feeds.

And it can read your **ON Later** list: it can list and search the pages
you've saved, and read or summarise one. If you tick **Save / archive**,
it can also save a link (with tags if you like), archive or un-archive a
page, and add or remove its tags. It can't delete anything.

Articles and saved pages were written by other people. ON Suite hands
their text to the assistant as outside content, so a web page can't give
the assistant instructions. More apps will follow.

For example, you can say "I'm on page 120 of Dune", "I finished Dune,
five stars", "save this quote from Dune: ...", "add milk to my groceries
list", "remind me to call Sam on Friday", "what's due this week?",
"tick off call Sam", "what's new in my feeds?", "summarise the article
about tapirs", "save this link to Later as recipes" or "tag that one
travel".

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

## Apps that can't sign in (personal tokens)

Some apps can't open ON Suite in your browser to sign in. The Pebble ring
is one: its phone app only lets you paste in a server address and a
fixed key. For those, you create a **personal token** yourself and paste
it into the app.

1. Open **Account** from the user menu and click **Connected
   assistants**, then **Create a token**.
2. Give it a name you'll recognise later, such as "Pebble ring".
3. Tick what it may do. The boxes are the same as on the permission
   screen (see below), and the ones that make changes start unticked. To
   add notes and record book progress from your ring, for example, tick
   **Add notes** and **Update books**.
4. Click **Create token**. ON Suite shows the token once. Copy it now:
   it can't be shown again.
5. In the app, add a custom MCP server. Use your ON Suite address
   followed by `/mcp` (for example `https://on.example.com/mcp`) and the
   header `Authorization: Bearer` followed by a space and your token.

A personal token doesn't expire on a date, but one that isn't used for
90 days is removed automatically. Changing your password removes it too.
It shows in **Connected assistants** as **Personal token**, and its
activity shows under **Recent activity** like any assistant's. Click
**Disconnect** to stop it straight away. If you lose the token, or want
to change what it can do, disconnect it and create a new one.

You can have up to 10 personal tokens.

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
**Update books** (for ON Books), **Add notes** (for ON Notes), **Mark
read / star** (for ON Reader) or **Save / archive** (for ON Later) when
you connected it. Check **Connected assistants** on
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

When an assistant reads an article you've saved or subscribed to, that text was
written by other people. ON Suite hands it over as outside content and tells
the assistant not to follow instructions in it. That makes it much harder for
a web page to steer the assistant, but not impossible, so only allow changes
you're comfortable with.
