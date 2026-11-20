#!/bin/sh
# Rebuilds the LibreOffice fixtures from their flat ODF sources:
#   testdata/xlsx/src/*.fods -> testdata/xlsx/*.xlsx
# A throwaway profile keeps LibreOffice away from the user's own settings.
set -e
cd "$(dirname "$0")"
profile=$(mktemp -d)
for f in src/*.fods; do
	soffice -env:UserInstallation="file://$profile" --headless --convert-to xlsx --outdir . "$f"
done
rm -rf "$profile"
