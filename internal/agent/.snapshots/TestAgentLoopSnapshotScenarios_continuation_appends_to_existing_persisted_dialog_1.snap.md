### stdout

````shell
continued answer
````

### stderr

````shell
> message_id: `msg_004`
````

### messages

| id        | parent_id | compaction_parent_id | role        | tool_result_error | message_extra_fields | model_ref | model_id | model_type | model_display_name | input_tokens | output_tokens | cache_read_tokens | cache_write_tokens |
|:----------|:----------|:---------------------|:------------|:------------------|:---------------------|:----------|:---------|:-----------|:-------------------|:-------------|:--------------|:------------------|:-------------------|
| "msg_001" | NULL      | NULL                 | "user"      | false             | NULL                 | NULL      | NULL     | NULL       | NULL               | NULL         | NULL          | NULL              | NULL               |
| "msg_002" | "msg_001" | NULL                 | "assistant" | false             | NULL                 | NULL      | NULL     | NULL       | NULL               | NULL         | NULL          | NULL              | NULL               |
| "msg_003" | "msg_002" | NULL                 | "user"      | false             | NULL                 | NULL      | NULL     | NULL       | NULL               | NULL         | NULL          | NULL              | NULL               |
| "msg_004" | "msg_003" | NULL                 | "assistant" | false             | NULL                 | NULL      | NULL     | NULL       | NULL               | NULL         | NULL          | NULL              | NULL               |

### blocks

| id   | message_id | block_type | modality_type | mime_type    | content             | extra_fields | sequence_order |
|:-----|:-----------|:-----------|:--------------|:-------------|:--------------------|:-------------|:---------------|
| NULL | "msg_001"  | "content"  | 0             | "text/plain" | "original question" | NULL         | 0              |
| NULL | "msg_002"  | "content"  | 0             | "text/plain" | "original answer"   | NULL         | 0              |
| NULL | "msg_003"  | "content"  | 0             | "text/plain" | "follow up"         | NULL         | 0              |
| NULL | "msg_004"  | "content"  | 0             | "text/plain" | "continued answer"  | NULL         | 0              |

