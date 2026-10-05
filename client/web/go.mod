// Not Go. This file only marks client/web as a separate module so the Go
// client's `./...` stops at it: npm packages occasionally ship .go files
// (node_modules/flatted does), and they must not become part of
// micro/client.
module micro/client/web
