#!/bin/sh
# Rebuilds the LibreOffice fixtures from their flat ODF sources:
#   testdata/docx/src/*.fodt -> testdata/docx/*.docx
# A throwaway profile keeps LibreOffice away from the user's own settings.
set -e
cd "$(dirname "$0")"
profile=$(mktemp -d)
for f in src/*.fodt; do
	soffice -env:UserInstallation="file://$profile" --headless --convert-to docx --outdir . "$f"
done
rm -rf "$profile"
