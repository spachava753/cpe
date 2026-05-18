### stdout

````shell

````

### stderr

````shell
#### [tool call]
```json
{
  "name": "compact_conversation",
  "parameters": {
    "summary": "first"
  }
}
```> message_id: `msg_002`> input: `18`, output: `64`
#### [tool call]
```json
{
  "name": "compact_conversation",
  "parameters": {
    "summary": "second"
  }
}
```> message_id: `msg_004`> input: `54`, output: `65`
````

### messages

| id        | parent_id | compaction_parent_id | role        | tool_result_error | message_extra_fields | model_ref | model_id | model_type | model_display_name | input_tokens | output_tokens | cache_read_tokens | cache_write_tokens |
|:----------|:----------|:---------------------|:------------|:------------------|:---------------------|:----------|:---------|:-----------|:-------------------|:-------------|:--------------|:------------------|:-------------------|
| "msg_001" | NULL      | NULL                 | "user"      | false             | NULL                 | NULL      | NULL     | NULL       | NULL               | NULL         | NULL          | NULL              | NULL               |
| "msg_002" | "msg_001" | NULL                 | "assistant" | false             | NULL                 | NULL      | NULL     | NULL       | NULL               | 18           | 64            | NULL              | NULL               |
| "msg_003" | NULL      | "msg_002"            | "user"      | false             | NULL                 | NULL      | NULL     | NULL       | NULL               | NULL         | NULL          | NULL              | NULL               |
| "msg_004" | "msg_003" | NULL                 | "assistant" | false             | NULL                 | NULL      | NULL     | NULL       | NULL               | 54           | 65            | NULL              | NULL               |

### blocks

| id       | message_id | block_type  | modality_type | mime_type    | content                                                                       | extra_fields | sequence_order |
|:---------|:-----------|:------------|:--------------|:-------------|:------------------------------------------------------------------------------|:-------------|:---------------|
| NULL     | "msg_001"  | "content"   | 0             | "text/plain" | "compact repeatedly"                                                          | NULL         | 0              |
| "call_1" | "msg_002"  | "tool_call" | 0             | "text/plain" | "{\"name\":\"compact_conversation\",\"parameters\":{\"summary\":\"first\"}}"  | NULL         | 0              |
| NULL     | "msg_003"  | "content"   | 0             | "text/plain" | "summary=first parent=msg_002 tool=compact_conversation"                      | NULL         | 0              |
| "call_2" | "msg_004"  | "tool_call" | 0             | "text/plain" | "{\"name\":\"compact_conversation\",\"parameters\":{\"summary\":\"second\"}}" | NULL         | 0              |

