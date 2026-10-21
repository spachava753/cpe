// Package models loads model configurations and persists the last-used model preference.
//
// The model configuration document must be UTF-8 and contain one JSON array of objects, with no
// trailing data. Each object requires base_url (an absolute HTTP(S) URL with a hostname), api, a
// nonblank id, nonempty input_modalities and output_modalities lists, and positive
// integer limit and max_tokens values. API names are case-sensitive: Responses,
// Anthropic, Gemini, Openai, Zai, Moonshot, Deepseek, Cerebras, and OpenRouter.
// Modalities are text, image, audio, or video; this package parses declarations
// without enforcing provider-specific capabilities.
//
// Optional input, output, cache_read, and cache_write costs are nonnegative
// numbers, defaulting to zero. An omitted or empty api_key produces no secret;
// other API keys are stored as secrecy.SecretString values. Optional
// reasoning_levels are provider-defined nonblank strings. reasoning_field is
// reasoning_details (the default when omitted or empty), reasoning_content, or
// reasoning. use_max_tokens defaults to false. extra_fields is an optional object
// of provider-specific JSON values, preserved without interpretation. Unknown
// model fields are rejected; provider-specific keys belong in extra_fields.
// Present configuration fields cannot be null, but values inside extra_fields
// may be null.
//
// LoadModels returns no models on any error and leaves the supplied file open.
package models
