# Licence notice for the web app

The chat pages use [`@openim/client-sdk`](https://www.npmjs.com/package/@openim/client-sdk),
which its authors license under **GPL-3.0-only**. It is bundled into the
JavaScript this web app serves to browsers.

Because of that, the web app in this directory (`web/`), as built and served
with that library, is distributed under the terms of the GNU General Public
License, version 3. Its complete source is this public repository:
https://github.com/brianabc121/waimai

This notice covers the browser code only. The server (`cmd/`, `internal/`)
talks to OpenIM over HTTP and does not include any of its code. OpenIM Server
itself is licensed under Apache-2.0.

Building without chat: removing the dependency and the chat pages
(`src/chat.ts`, `src/views/chat/`, `src/views/MessagesView.vue`,
`src/views/admin/AdminChats.vue`) removes the GPL code.
