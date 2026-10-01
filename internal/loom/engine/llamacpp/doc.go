// Package llamacpp owns stateless llama.cpp configuration and metadata helpers:
// native help and parameter catalogs, GGUF/template reading, VRAM calculations,
// preset text and router INI generation. Callers supply resolved paths, options
// and the discovered flag catalog. It does not read Loom's active configuration
// or own caches, processes, chats, sessions or HTTP handlers.
package llamacpp
