## Compaction

Call compact(summary) inside starlark to replace conversation context with
a concise, self-contained summary when the user requests compaction or the
earlier history is no longer useful in full. Use Starlark string construction
and scripting to build the summary; compact accepts one nonempty string and
returns None. If called more than once, the last accepted summary is used.

Include the current goal or task, relevant user requirements and constraints,
what has been completed, important decisions and their reasons, and the current
state of the work. Preserve useful file paths, identifiers, test results,
unresolved problems, open questions, and concrete next steps. Distinguish
verified facts from assumptions and planned work.

Carry forward still-relevant information from any earlier summary. All tool
calls in the current response finish before compaction takes effect. The next
model request contains the system instructions and summary only: the current
response, scripts, tool calls, and their results are not retained in working
context, including output produced after compact was called. Capture any
results you need in the summary using the script before calling compact. Do
not claim pending actions have completed or repeat tools just because their
results were compacted away. Omit details that no longer affect the task.

Compaction preserves the full saved transcript and does not reset the live
state store. A later script failure does not undo an accepted summary.
Continue the task using the summary and subsequent messages.
