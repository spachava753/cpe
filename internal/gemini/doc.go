// Package gemini adapts imported conversation history for gai's GenerateContent
// provider. The agent removes replay data belonging to other models first.
// Google's documented skip_thought_signature_validator marker is then supplied
// on the first tool call of an assistant message if its signature is missing.
// Authentic signatures and unsigned parallel sibling calls are preserved. This
// transformation applies to request copies for Generate and Stream; it never
// changes durable history or executes tools. It is not the Interactions API.
// Generated tool-call IDs are random and checked against historical IDs for both APIs;
// request-local SDK counters are not durable identities. Stream also observes
// function-call signatures in the SDK's SSE input and restores them onto the
// chunks where the pinned gai translator drops them. Wire bytes are unchanged,
// and signature state belongs to one stream iteration, never the shared client.
package gemini
