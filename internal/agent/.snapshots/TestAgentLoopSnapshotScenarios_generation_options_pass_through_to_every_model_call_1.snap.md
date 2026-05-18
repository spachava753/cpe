### stdout

````shell
  configured answer

````

### stderr

````shell
  #### [tool call]
  
    {
      "name": "lookup",
      "parameters": {
        "q": "generation parameters"
      }
    }

  
  | message_id: msg_002

  
  | input: 57, output: 60



  #### Tool "lookup" result:
  
    lookup result

  
  | message_id: msg_003

  
  | message_id: msg_004

  
  | input: 130, output: 17


````

### generation options

| call | temperature | top_p | top_k | frequency_penalty | presence_penalty | n | max_generation_tokens | tool_choice | stop_sequences | output_modalities | audio_config                          | thinking_budget | extra_args                                      |
|:-----|:------------|:------|:------|:------------------|:-----------------|:--|:----------------------|:------------|:---------------|:------------------|:--------------------------------------|:----------------|:------------------------------------------------|
| 1    | 0.37        | 0.81  | 40    | 0.2               | 0.4              | 2 | 512                   | "lookup"    | ["END","STOP"] | ["text","audio"]  | {"voice_name":"alloy","format":"wav"} | "medium"        | {"provider_flag":true,"provider_mode":"strict"} |
| 2    | 0.37        | 0.81  | 40    | 0.2               | 0.4              | 2 | 512                   | "lookup"    | ["END","STOP"] | ["text","audio"]  | {"voice_name":"alloy","format":"wav"} | "medium"        | {"provider_flag":true,"provider_mode":"strict"} |

### messages

| id        | parent_id | compaction_parent_id | role          | tool_result_error | message_extra_fields | model_ref | model_id | model_type | model_display_name | input_tokens | output_tokens | cache_read_tokens | cache_write_tokens |
|:----------|:----------|:---------------------|:--------------|:------------------|:---------------------|:----------|:---------|:-----------|:-------------------|:-------------|:--------------|:------------------|:-------------------|
| "msg_001" | NULL      | NULL                 | "user"        | false             | NULL                 | NULL      | NULL     | NULL       | NULL               | NULL         | NULL          | NULL              | NULL               |
| "msg_002" | "msg_001" | NULL                 | "assistant"   | false             | NULL                 | NULL      | NULL     | NULL       | NULL               | 57           | 60            | NULL              | NULL               |
| "msg_003" | "msg_002" | NULL                 | "tool_result" | false             | NULL                 | NULL      | NULL     | NULL       | NULL               | NULL         | NULL          | NULL              | NULL               |
| "msg_004" | "msg_003" | NULL                 | "assistant"   | false             | NULL                 | NULL      | NULL     | NULL       | NULL               | 130          | 17            | NULL              | NULL               |

### blocks

| id       | message_id | block_type  | modality_type | mime_type    | content                                                                  | extra_fields | sequence_order |
|:---------|:-----------|:------------|:--------------|:-------------|:-------------------------------------------------------------------------|:-------------|:---------------|
| NULL     | "msg_001"  | "content"   | 0             | "text/plain" | "answer with configured generation parameters after lookup"              | NULL         | 0              |
| "call_1" | "msg_002"  | "tool_call" | 0             | "text/plain" | "{\"name\":\"lookup\",\"parameters\":{\"q\":\"generation parameters\"}}" | NULL         | 0              |
| "call_1" | "msg_003"  | "content"   | 0             | "text/plain" | "lookup result"                                                          | NULL         | 0              |
| NULL     | "msg_004"  | "content"   | 0             | "text/plain" | "configured answer"                                                      | NULL         | 0              |

