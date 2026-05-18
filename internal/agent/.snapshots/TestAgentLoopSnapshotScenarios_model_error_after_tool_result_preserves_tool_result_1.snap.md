### stdout

````shell

````

### stderr

````shell
#### [tool call]
```json
{
  "name": "lookup",
  "parameters": {}
}
```> message_id: `msg_002`> input: `21`, output: `33`

#### Tool "lookup" result:

```
lookup result
```> message_id: `msg_003`
````

### generation options

| call | temperature | top_p | top_k | frequency_penalty | presence_penalty | n    | max_generation_tokens | tool_choice | stop_sequences | output_modalities | audio_config | thinking_budget | extra_args |
|:-----|:------------|:------|:------|:------------------|:-----------------|:-----|:----------------------|:------------|:---------------|:------------------|:-------------|:----------------|:-----------|
| 1    | NULL        | NULL  | NULL  | NULL              | NULL             | NULL | NULL                  | NULL        | NULL           | NULL              | NULL         | NULL            | NULL       |
| 2    | NULL        | NULL  | NULL  | NULL              | NULL             | NULL | NULL                  | NULL        | NULL           | NULL              | NULL         | NULL            | NULL       |

### messages

| id        | parent_id | compaction_parent_id | role          | tool_result_error | message_extra_fields | model_ref | model_id | model_type | model_display_name | input_tokens | output_tokens | cache_read_tokens | cache_write_tokens |
|:----------|:----------|:---------------------|:--------------|:------------------|:---------------------|:----------|:---------|:-----------|:-------------------|:-------------|:--------------|:------------------|:-------------------|
| "msg_001" | NULL      | NULL                 | "user"        | false             | NULL                 | NULL      | NULL     | NULL       | NULL               | NULL         | NULL          | NULL              | NULL               |
| "msg_002" | "msg_001" | NULL                 | "assistant"   | false             | NULL                 | NULL      | NULL     | NULL       | NULL               | 21           | 33            | NULL              | NULL               |
| "msg_003" | "msg_002" | NULL                 | "tool_result" | false             | NULL                 | NULL      | NULL     | NULL       | NULL               | NULL         | NULL          | NULL              | NULL               |

### blocks

| id       | message_id | block_type  | modality_type | mime_type    | content                                   | extra_fields | sequence_order |
|:---------|:-----------|:------------|:--------------|:-------------|:------------------------------------------|:-------------|:---------------|
| NULL     | "msg_001"  | "content"   | 0             | "text/plain" | "tool then model fails"                   | NULL         | 0              |
| "call_1" | "msg_002"  | "tool_call" | 0             | "text/plain" | "{\"name\":\"lookup\",\"parameters\":{}}" | NULL         | 0              |
| "call_1" | "msg_003"  | "content"   | 0             | "text/plain" | "lookup result"                           | NULL         | 0              |

