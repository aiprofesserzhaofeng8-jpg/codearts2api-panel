# Sub2API Responses compatibility

Source: https://github.com/Wei-Shaw/sub2api
Revision: ba81c7fd758e425d67f856d601147ef0c5263d3f

The Go files in this directory are derived from backend/internal/pkg/apicompat,
licensed under LGPL-3.0 (LICENSE), incorporating GPL-3.0 (COPYING).
Original authors: Wei-Shaw and Sub2API contributors.

Selected conversion files are copied verbatim; transport, account management and
validation live in internal/server. Source and build.sh allow rebuilding with
a modified compatibility library. See helpers.go for extracted shared helpers.
