### stdout

````shell
  hi there

````

### stderr

````shell
  
  | message_id: msg_002

  
  | input: 5, output: 8


````

### generation options

| call | temperature | top_p | top_k | frequency_penalty | presence_penalty | n    | max_generation_tokens | tool_choice | stop_sequences | output_modalities | audio_config | thinking_budget | extra_args |
|:-----|:------------|:------|:------|:------------------|:-----------------|:-----|:----------------------|:------------|:---------------|:------------------|:-------------|:----------------|:-----------|
| 1    | NULL        | NULL  | NULL  | NULL              | NULL             | NULL | NULL                  | NULL        | NULL           | NULL              | NULL         | NULL            | NULL       |

### messages

| id        | parent_id | compaction_parent_id | role        | tool_result_error | message_extra_fields | model_ref | model_id | model_type | model_display_name | input_tokens | output_tokens | cache_read_tokens | cache_write_tokens |
|:----------|:----------|:---------------------|:------------|:------------------|:---------------------|:----------|:---------|:-----------|:-------------------|:-------------|:--------------|:------------------|:-------------------|
| "msg_001" | NULL      | NULL                 | "user"      | false             | NULL                 | NULL      | NULL     | NULL       | NULL               | NULL         | NULL          | NULL              | NULL               |
| "msg_002" | "msg_001" | NULL                 | "assistant" | false             | NULL                 | NULL      | NULL     | NULL       | NULL               | 5            | 8             | NULL              | NULL               |

### blocks

| id   | message_id | block_type | modality_type | mime_type    | content    | extra_fields | sequence_order |
|:-----|:-----------|:-----------|:--------------|:-------------|:-----------|:-------------|:---------------|
| NULL | "msg_001"  | "content"  | 0             | "text/plain" | "hello"    | NULL         | 0              |
| NULL | "msg_002"  | "content"  | 0             | "text/plain" | "hi there" | NULL         | 0              |

