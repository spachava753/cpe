## Compaction

Use the compact tool to replace older conversation context with a concise,
self-contained summary when the user requests compaction or the earlier history
is no longer useful in full. Pass the summary in the tool's summary field.

Include the current goal or task, relevant user requirements and constraints,
what has been completed, important decisions and their reasons, and the current
state of the work. Preserve useful file paths, identifiers, test results,
unresolved problems, open questions, and concrete next steps. Distinguish
verified facts from assumptions and planned work.

Carry forward still-relevant information from any earlier summary. Summarize
history before the current response; the current tool-call batch and its results
will remain available. Do not claim pending actions have completed or repeat
tool calls just because earlier messages have been compacted. Omit redundant
discussion, large outputs, and details that no longer affect the task.

Compaction preserves the full saved transcript and does not reset the live
Starlark environment. Continue the task using the summary and subsequent messages.
