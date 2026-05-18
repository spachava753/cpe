### stdout

````shell
  used warned result

````

### stderr

````shell
  #### [tool call]
  
    {
      "name": "lookup",
      "parameters": {}
    }

  
  | message_id: msg_002

  
  | input: 18, output: 33



  #### Tool "lookup" result:
  
    [COMPACTION WARNING]
    The conversation has exceeded the configured compaction threshold. Before continuing much further, call the compact_conversation tool with a compact but complete summary of the conversation state needed to continue. This warning will continue to appear until compaction is performed.
  
    lookup result

  
  | message_id: msg_003

  
  | message_id: msg_004

  
  | input: 368, output: 18


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
| "msg_002" | "msg_001" | NULL                 | "assistant"   | false             | NULL                 | NULL      | NULL     | NULL       | NULL               | 18           | 33            | NULL              | NULL               |
| "msg_003" | "msg_002" | NULL                 | "tool_result" | false             | NULL                 | NULL      | NULL     | NULL       | NULL               | NULL         | NULL          | NULL              | NULL               |
| "msg_004" | "msg_003" | NULL                 | "assistant"   | false             | NULL                 | NULL      | NULL     | NULL       | NULL               | 368          | 18            | NULL              | NULL               |

### blocks

| id       | message_id | block_type  | modality_type | mime_type    | content                                                                                                                                                                                                                                                                                                             | extra_fields | sequence_order |
|:---------|:-----------|:------------|:--------------|:-------------|:--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|:-------------|:---------------|
| NULL     | "msg_001"  | "content"   | 0             | "text/plain" | "large conversation"                                                                                                                                                                                                                                                                                                | NULL         | 0              |
| "call_1" | "msg_002"  | "tool_call" | 0             | "text/plain" | "{\"name\":\"lookup\",\"parameters\":{}}"                                                                                                                                                                                                                                                                           | NULL         | 0              |
| "call_1" | "msg_003"  | "content"   | 0             | "text/plain" | "[COMPACTION WARNING]\nThe conversation has exceeded the configured compaction threshold. Before continuing much further, call the compact_conversation tool with a compact but complete summary of the conversation state needed to continue. This warning will continue to appear until compaction is performed." | NULL         | 0              |
| "call_1" | "msg_003"  | "content"   | 0             | "text/plain" | "lookup result"                                                                                                                                                                                                                                                                                                     | NULL         | 1              |
| NULL     | "msg_004"  | "content"   | 0             | "text/plain" | "used warned result"                                                                                                                                                                                                                                                                                                | NULL         | 0              |

