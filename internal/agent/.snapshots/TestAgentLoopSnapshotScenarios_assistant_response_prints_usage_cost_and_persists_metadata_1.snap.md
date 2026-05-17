### stdout

````shell
usage tracked
````

### stderr

````shell
> message_id: `msg_002`> input: `40`, output: `10`, cache read: `5`, cache write: `3`
> context: `50 / 100` (`50.00%`)
> estimated cost: input: `$0.000064`, output: `$0.000040`, cache read: `$0.000003`, cache write: `$0.000003`, total: `$0.000110`, cumulative: `$0.000110`
````

### messages

| id        | parent_id | compaction_parent_id | role        | tool_result_error | message_extra_fields | model_ref    | model_id           | model_type      | model_display_name    | input_tokens | output_tokens | cache_read_tokens | cache_write_tokens |
|:----------|:----------|:---------------------|:------------|:------------------|:---------------------|:-------------|:-------------------|:----------------|:----------------------|:-------------|:--------------|:------------------|:-------------------|
| "msg_001" | NULL      | NULL                 | "user"      | false             | NULL                 | NULL         | NULL               | NULL            | NULL                  | NULL         | NULL          | NULL              | NULL               |
| "msg_002" | "msg_001" | NULL                 | "assistant" | false             | NULL                 | "test-model" | "provider-model-1" | "test-provider" | "Test Provider Model" | 40           | 10            | 5                 | 3                  |

### blocks

| id   | message_id | block_type | modality_type | mime_type    | content         | extra_fields | sequence_order |
|:-----|:-----------|:-----------|:--------------|:-------------|:----------------|:-------------|:---------------|
| NULL | "msg_001"  | "content"  | 0             | "text/plain" | "show usage"    | NULL         | 0              |
| NULL | "msg_002"  | "content"  | 0             | "text/plain" | "usage tracked" | NULL         | 0              |

