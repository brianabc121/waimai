#!/bin/bash
# Installs the debug APK on the running emulator and runs e2e/android.cjs.
# Called by .github/workflows/android-emulator.yml once the emulator is up.
set -euo pipefail
PKG=${WORKBENCH_APP_ID:-app.waimai.workbench}
adb install -r mobile/android/app/build/outputs/apk/debug/app-debug.apk
# What a rider would allow on first use. The dialogs belong to Android, not to the app.
for p in ACCESS_FINE_LOCATION ACCESS_COARSE_LOCATION POST_NOTIFICATIONS; do
  adb shell pm grant "$PKG" "android.permission.$p"
done
adb shell settings put secure location_mode 3
node e2e/android.cjs || { adb logcat -d -t 300 '*:W' | grep -iE "capacitor|chromium|$PKG" | tail -80 || true; exit 1; }
