# Voice & Tone

Internal style guide for Cascade's documentation. Not published — this lives at
the `docs/` root, outside `docs/public/`, so contributors (and Claude) can edit
the public docs against a shared standard.

The goal: docs that read like a competent engineer wrote them for another
engineer. Warm and direct, never breathless. The test for any sentence is "would
a senior developer say this out loud to a colleague?" If it sounds like a product
launch or a chatbot, rewrite it.

## Voice

- **Warm and direct.** Address the reader as "you." Be confident about what
  Cascade does without selling it. Plain statements beat hype.
- **Concrete over abstract.** Name the file, the command, the setting. "Drop a
  `.go` file in the scripts folder" beats "leverage the scripting subsystem."
- **Respect the reader's time.** They came for an answer. Lead with it.

## The AI tells to cut

These are the patterns that make docs read as machine-generated. Cut them on
sight.

### 1. Em-dash drama

The single biggest tell. AI prose reaches for an em-dash in nearly every
sentence to stage a dramatic pause or tack on an aside.

- An em-dash is fine **occasionally** — maybe once every few paragraphs.
- It is not the default connective. Most em-dashes should become a period, a
  comma, or a colon.

> ❌ You write plain Go, Cascade interprets it in-process — and your script
> reacts to channel events or fires proactively on a timer — think of it as one
> rung below the plugin system.
>
> ✅ You write plain Go and Cascade runs it in-process. Your script can react to
> channel events or fire on a timer. It sits a level below the plugin system.

### 2. Bold-stacking for emphasis

AI bolds individual words mid-sentence to manufacture importance: "what Cascade
does **today**", "tracks what's **next**", "the build is **not code-signed**".

- Reserve bold for genuine UI labels, key terms on first use, or table headers.
- If a sentence has more than one bolded fragment, you're shouting. Delete the
  emphasis and let the words stand.

### 3. The "not X — Y" reversal

"Errors surface immediately, not at 2am when someone says the wrong thing."
"No separate process, no IPC protocol, no binary to ship." The negation-then-
reveal and the rule-of-three flourish are both AI signatures.

- State the positive directly: "Errors surface at load time, in the Scripts
  panel."
- One parallel pair is human. Three is a tell.

### 4. Single-word italic emphasis

"The absence of those packages *is* the sandbox." Italicizing one word for
gravitas is a tell. Restructure so the sentence carries the weight on its own.

### 5. Clever asides and metaphors

"Table-stakes." "Mop up the cheap items." "One rung below." "Power-vs-simplicity
axis." "Not at 2am when someone says the wrong thing." These read as the model
performing wit.

- Cut the metaphor or replace it with the literal thing it stands for.
- Documentation does not need a joke in it to be good.

### 6. Self-congratulatory meta-commentary

"Every entry here was verified by grep." "This document tracks what's next." The
docs narrating their own diligence or structure is a tell — and usually filler.

- Trust the reader to see the structure. Cut sentences that only describe the
  page.

### 7. Hedging throat-clearing

"It's worth noting that…", "Essentially…", "In essence…", "Think of it as…".
Delete the preamble and start with the content.

## Mechanical rules

- **Sentences over fragments staged with dashes.** When tempted by an em-dash,
  try a period first.
- **One idea per sentence.** AI chains clauses; break them.
- **Keep all technical content.** De-AI-ing is about prose texture, not facts.
  Never drop a file path, command, caveat, or capability while rewriting.
- **Active voice, present tense.** "Cascade reads those files" not "those files
  are read."
- **Don't over-format.** Not every list needs to be a table; not every term
  needs bold.

## Before / after

> ❌ Cascade Scripting brings mIRC-spirited automation directly into the client —
> no separate process, no IPC protocol, no binary to ship. You write plain Go,
> Cascade interprets it in-process, and your script reacts to channel events or
> fires proactively on a timer. Think of it as one rung below the plugin system:
> a lightweight, statically-typed, instant-feedback way to automate personal
> workflows you trust.

> ✅ Cascade Scripting puts mIRC-style automation right in the client, with no
> separate process to run or protocol to learn. You write plain Go; Cascade runs
> it in-process, where it can react to channel events or fire on a timer. Scripts
> sit a level below the plugin system: a lightweight, statically typed way to
> automate the personal workflows you trust.

The second version keeps the warmth and the second-person address. It just stops
reaching for a device in every clause.

## Diagrams

Use Mermaid, not ASCII art. Zensical renders ` ```mermaid ` fenced blocks to
themed SVG out of the box (no config or dependency to add), and the source stays
diffable plain text in the markdown.

- **Architecture / wiring** → `flowchart`. Group related nodes with `subgraph`.
- **A thing that changes state** → `stateDiagram-v2` (e.g. a lifecycle with
  statuses and transitions). Far clearer than a status table plus scattered
  prose.
- **A protocol or handshake over time** → `sequenceDiagram`. Use solid arrows
  (`->>`) for requests, dashed (`-->>`, `--)`) for responses and notifications,
  and `alt` blocks for conditional branches.

Reach for a diagram when the prose is describing a *shape* — a flow, a sequence,
a state machine, a hierarchy — that the reader otherwise has to reassemble in
their head. Don't diagram something a short list or table already makes obvious.

One exception stays ASCII: **file trees** (`├──`, `└──`). Mermaid has no good
primitive for them, so the ASCII form is the right call.

A diagram is an aid, not the source of truth. Keep the surrounding prose
self-sufficient so the page still reads correctly if the SVG fails to load.
