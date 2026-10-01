// Package llamacpp owns llama.cpp configuration, metadata and engine execution:
// native help and parameter catalogs, GGUF/template reading, VRAM calculations,
// preset text, router management and child supervision with observation caches.
// Callers supply resolved paths, configuration/state accessors, launch environment
// and application cleanup callbacks. The package has no runtime globals and does
// not read Loom's active configuration or own services, chats or HTTP handlers.
package llamacpp
