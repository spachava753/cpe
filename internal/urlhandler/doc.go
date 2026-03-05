/*
Package urlhandler validates HTTP(S) inputs and downloads remote content with
timeouts, retry/backoff, and maximum-size enforcement.

It is used when user inputs include URLs so remote resources can be converted
into model input blocks without duplicating networking logic in higher layers.
*/
package urlhandler
