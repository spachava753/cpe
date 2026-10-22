# Usage and context limits

[All guides](README.md)

CPE reports two different token measurements: the size of the latest measured
model context, and the conversation's accumulated provider-reported consumption.
Both are useful, but they answer different questions.

## How full is the context?

The context meter uses the latest successful generation's input plus output
tokens. Its capacity is the selected model's configured context limit.

```text
 Latest successful generation

 [input: guidance + working conversation] [output: model response]
 |<-------------------- used ------------------------------->|
 |<---------------- configured context capacity -------------------->|
```

Cached input still occupies context. A provider may charge differently for it,
but it still counts toward how much the model can see.

CPE sends this measurement through ACP usage updates when both input and output
counts are available. It does not invent a measurement at startup or when a
session is restored. Tool output, compaction, and model changes become visible
in the meter after the next measured successful generation.

For example, a large tool result can enter the conversation after the meter was
last updated. The meter will still show the preceding generation's usage until
a later response reports new counts. CPE does not make an extra provider request
to count tokens or estimate tool-result size.

## How much has the conversation consumed?

The usage returned at the end of a prompt accumulates reported consumption
across the saved conversation. It includes all turns and generation attempts,
including retries and canceled attempts when their usage was reported.

```text
 Turn 1: attempt A + attempt B + next generation
 Turn 2: first generation + next generation
 Turn 3: reported usage before cancellation
         |
         v
 Cumulative conversation usage
```

Totals survive loading, resuming, and forking. A fork begins with the usage of
its inherited conversation, then accumulates its own later usage. Adding the
parent and fork totals together would count their shared past twice.

Input and output are added once to produce total tokens. Cache reads, cache
writes, and reasoning counts are breakdowns within those totals. Adding them
again would overcount.

Missing categories remain unreported. CPE omits the usage object until input
and output totals have both been reported. If a provider never supplies usage
for an interrupted attempt, CPE cannot reconstruct it. Consumption figures are
therefore limited by the measurements the provider supplies.

## When context gets tight

At 85% of the configured context window, based on the latest successful input
plus output measurement, CPE adds a warning to tool results asking the model to
finish the current task and compact.

The warning leaves the turn running. The model needs to write a summary and
request compaction; there is no automatic summary generation or forced stop.
You can ask for `/compact` earlier, especially before a large task or a switch
to a smaller model. See [Compaction](compaction.md).

Compaction can reduce the next context measurement. Accumulated consumption
continues to include the earlier work. The transcript also remains saved, so a
smaller context does not mean a smaller saved conversation.

## Costs and limits

CPE omits billed cost because it has no reliable billing data. Configured list
prices alone cannot account for what the provider actually charged.

The model catalog's required `limit` and `max_tokens` values supply the context
and requested output limits in tokens. There is no universal default. The
85% warning threshold is rounded up to a whole token: a configured window of
128,000 tokens produces a warning at **108,800 tokens**. Provider behavior still
determines whether a request fits and how it finishes.

CPE imposes no size cap on prompt text, input images, or command stdin.
Emitted script text longer than **128 KiB** is saved in full in session KV;
the model receives a preview and retrieval notice within that 128 KiB allowance.
Each emitted image has a **6 MiB raw-byte limit**. CPE has no combined output-byte
or block-count cap. See [Limits and defaults](limits.md) for the complete values,
overflow behavior, and configurable settings.
