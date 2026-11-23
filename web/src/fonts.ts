// Burmese and Khmer fonts are missing on some Android phones and older
// computers, so the site carries its own (served from /assets, never from
// Google: it is blocked in some markets). Loaded only for those languages;
// each file covers its script alone (unicode-range), so nothing else changes.
import '@fontsource/noto-sans-myanmar/400.css'
import '@fontsource/noto-sans-myanmar/700.css'
import '@fontsource/noto-sans-khmer/400.css'
import '@fontsource/noto-sans-khmer/700.css'
