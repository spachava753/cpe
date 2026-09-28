# CPE Architecure

This document is a technical overview of the architecture of CPE. This document is heavily inspired by [TigerBeetle documentation](https://github.com/tigerbeetle/tigerbeetle/blob/main/docs/ARCHITECTURE.md).

## Problem Statement

CPE is a general agent harness. It departs from the idea that it is simply another CLI agent harness, as it tries to solve the following problems with different layers in it's stack:

- a TUI-based agent
- an ACP server that can be run as a CLI subprocess, HTTP server, or in-procress via in-memory transport
- multi-model support and the ability to flexibily swap models and providers during execution
- support multiple subscription based providers like ChatGPT subscription, OpenCode Go subscription, Z.ai Coding plan, etc.
- supporting common standards like [ACP](https://agentclientprotocol.com/get-started/introduction), [MCP](https://modelcontextprotocol.io/docs/2026-07-28/getting-started/intro), [AGENTS.md](https://agents.md/), [skills](http://agentskills.md/), and [agent plugins](https://agent-plugins.org/) out of the box
- support customization of agent runtime configuration; the system prompt, tools (via MCP), and user provided hooks during the agent loop
- support for modern harness techniques, like programatic tool calling, RLMs, and searching through conversations to recall memory

## Motivation

There are many agent runtimes that can do some, if not all of the above, out of the box, or via customization. Here is a short, non-exhastive list:

- Claude Code
- Codex
- Grok Build
- OpenCode
- Pi
- Goose
- AntiGravity
- ZCode

CPE is yet another agent harness that aims to complement the list. One of the main motiviating reason to see the existence of an agent harness written in idiomatic Go, with the flexibility and customizability of something like that of Pi.

## Design

TODO
