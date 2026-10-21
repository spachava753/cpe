# Compaction

[All guides](README.md)

Compaction lets a long-running conversation continue with a shorter working
context. The model writes a summary of the information it needs next. CPE uses
that summary in place of earlier messages on subsequent model requests, while
keeping the full accepted transcript in the saved session.

## Two views of the conversation

```text
 Before compaction

 Saved transcript:  [prompt] [response] [tools] [results] [more work ...]
 Model context:     [instructions] + all of that conversation

 After compaction

 Saved transcript:  [prompt] [response] [tools] [results] [more work ...]
                                      still retained

 Model context:     [instructions] [summary] [new messages ...]
```

The saved transcript remains available for client replay when a session is
loaded. The model continues from the summary and subsequent messages. Facts
omitted from the summary leave its immediate context, even though they remain
in the transcript.

This makes summary quality important. Useful summaries preserve the goal,
constraints, completed work, decisions and their reasons, unresolved questions,
and concrete next steps. They distinguish observed results from assumptions
and planned actions. A second compaction also needs to carry forward anything
still useful from the earlier summary.

## Asking for compaction

Send `/compact` to ask the model to summarize the conversation. You can add
instructions, such as `/compact keep the failed test and the next debugging step`.
The command asks the model to call `compact(summary)` inside a Starlark script.
The model can also choose to compact during ordinary work.

The call accepts a nonempty summary. If several summaries are accepted before
the next compaction takes effect, the last one is used. An invalid call produces
a tool error. A later script failure leaves an already accepted summary pending.

CPE gives the model summary-writing guidance from the start of the conversation.
`~/.cpe/COMPACTION.md` can replace that guidance independently of the main system
prompt. See [Instructions and context](instructions.md).

## Exactly when the summary takes over

Compaction takes effect after the current model response's tool calls finish or
are settled as interrupted. It includes the entire response and its tool results
in the portion replaced by the summary.

```text
 Model response requests tools
       |
       +-- Script A
       |     gather results
       |     compact(summary)
       |     remaining script statements
       |
       +-- Other requested calls finish
       |
       v
 Save compaction boundary and summary
       |
       v
 Next model request: instructions + summary
```

The next request omits the current response, its scripts, and their results from
working context. This includes output emitted after `compact` was called and
results from other calls in that response.

For example, a script might run a test, print its result, then call `compact`.
If the summary omits that test result, the next model request will not contain
it. The script needs to incorporate the result into the summary before making
the call. Likewise, a summary cannot safely claim that a later pending command
has already succeeded.

CPE saves the summary before the next model request. If the turn stops first,
it attempts to save it after outstanding calls are settled during turn-end
cleanup. A failure to save the compaction prevents the next generation from
proceeding with it. The [recovery guide](persistence.md) describes checkpoint
failures more generally.

## What stays in place

Compaction leaves the following intact:

- The full saved transcript and measured usage totals.
- The session's durable key-value data.
- The selected model and reasoning setting.
- Live session resources, including MCP connections.
- File changes and external effects from earlier work.

Loading, resuming, and forking preserve the compacted working context. They do
not rerun the historical tools. Scripts still start with fresh variables on
every invocation, as described in [Tool execution](tool-runtime.md).

## The context warning

When the latest successful model response reports input plus output usage at
85% or more of the configured context window, CPE adds a warning to tool results.
It asks the model to finish the current task and compact. Cached input counts
toward that threshold.

The warning is advisory. CPE continues the turn and leaves summary writing to
the model. There is no automatic compaction or separate summarization request.
The warning relies on reported token counts; newly produced tool output can
consume space before a later generation measures it. See
[Usage and context limits](usage.md).
