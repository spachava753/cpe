### stdout

````shell
partial output
````

### stderr

````shell
> input: `12`, output: `14`
````

### messages

| id        | parent_id | compaction_parent_id | role   | tool_result_error | message_extra_fields | model_ref | model_id | model_type | model_display_name | input_tokens | output_tokens | cache_read_tokens | cache_write_tokens |
|:----------|:----------|:---------------------|:-------|:------------------|:---------------------|:----------|:---------|:-----------|:-------------------|:-------------|:--------------|:------------------|:-------------------|
| "msg_001" | NULL      | NULL                 | "user" | false             | NULL                 | NULL      | NULL     | NULL       | NULL               | NULL         | NULL          | NULL              | NULL               |

### blocks

| id   | message_id | block_type | modality_type | mime_type    | content        | extra_fields | sequence_order |
|:-----|:-----------|:-----------|:--------------|:-------------|:---------------|:-------------|:---------------|
| NULL | "msg_001"  | "content"  | 0             | "text/plain" | "stream fails" | NULL         | 0              |

