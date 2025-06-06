#!/bin/bash

CONFIG_FILE="./scraper.yaml"
DIR="./internal/scraper/sites"
FACTORY_FILE="$DIR/abstract_factory.go"
TMP_FILE="$DIR/abstract_factory.tmp"

echo "package scraper" > "$TMP_FILE"
echo "" >> "$TMP_FILE"

# Generate const enums for scraper keys
echo "const (" >> "$TMP_FILE"
yq -r '.scrapers[] | "	Scraper" + (.key | gsub("[^a-zA-Z0-9]"; "_") | ascii_upcase) + " = \"" + .key + "\""' "$CONFIG_FILE" >> "$TMP_FILE"
echo ")" >> "$TMP_FILE"
echo "" >> "$TMP_FILE"

# Generate usecaseToScraper map
echo "var usecaseToScraper = map[string]string{" >> "$TMP_FILE"
yq -r '.scrapers[] | select(.usecase) | "	\"" + .usecase + "\": Scraper" + (.key | gsub("[^a-zA-Z0-9]"; "_") | ascii_upcase) + ","' "$CONFIG_FILE" >> "$TMP_FILE"
echo "}" >> "$TMP_FILE"
echo "" >> "$TMP_FILE"

echo "func NewScraper(usecase string) Scraper {" >> "$TMP_FILE"
echo "	scraperKey, ok := usecaseToScraper[usecase]" >> "$TMP_FILE"
echo "	if !ok {" >> "$TMP_FILE"
echo "		return nil" >> "$TMP_FILE"
echo "	}" >> "$TMP_FILE"
echo "" >> "$TMP_FILE"
echo "	switch scraperKey {" >> "$TMP_FILE"
yq -r '.scrapers[] | "	case Scraper" + (.key | gsub("[^a-zA-Z0-9]"; "_") | ascii_upcase) + ":\n		return &" + .name + "{}"' "$CONFIG_FILE" >> "$TMP_FILE"
echo "	default:" >> "$TMP_FILE"
echo "		return nil" >> "$TMP_FILE"
echo "	}" >> "$TMP_FILE"
echo "}" >> "$TMP_FILE"

mv "$TMP_FILE" "$FACTORY_FILE"
echo "✅ Factory generated in $FACTORY_FILE"
