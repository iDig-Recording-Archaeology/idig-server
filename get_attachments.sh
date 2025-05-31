#!/bin/bash

# Simple script to extract all attachments from idig server
BASE_URL="https://enki.agathe.gr"
USERNAME="bruce"
PASSWORD="agorapass"

echo "PROJECT	TRENCH	NAME	CHECKSUM" > attachments.txt

echo "Getting list of all trenches..." >&2
TRENCHES=$(curl -s -u "$USERNAME:$PASSWORD" "$BASE_URL/idig")

echo "$TRENCHES" | python3 -c "
import json
import sys

# Get trenches list
trenches_data = json.load(sys.stdin)

for trench in trenches_data['trenches']:
    project = trench['project']
    trench_name = trench['name']
    print(f'{project}\t{trench_name}')
" | while IFS=$'\t' read -r PROJECT TRENCH; do
    echo "Processing: $PROJECT/$TRENCH" >&2
    
    # URL encode for curl
    ENCODED_PROJECT=$(printf '%s' "$PROJECT" | python3 -c "import sys, urllib.parse; print(urllib.parse.quote(sys.stdin.read().strip()))")
    ENCODED_TRENCH=$(printf '%s' "$TRENCH" | python3 -c "import sys, urllib.parse; print(urllib.parse.quote(sys.stdin.read().strip()))")
    
    # Get surveys using curl
    SURVEYS_URL="$BASE_URL/idig/$ENCODED_PROJECT/$ENCODED_TRENCH/surveys"
    SURVEYS_JSON=$(curl -s -u "$USERNAME:$PASSWORD" "$SURVEYS_URL" 2>/dev/null)
    
    if [ $? -eq 0 ] && [ -n "$SURVEYS_JSON" ]; then
        echo "$SURVEYS_JSON" | python3 -c "
import json
import sys

try:
    data = json.load(sys.stdin)
    for survey in data.get('surveys', []):
        relation_attachments = survey.get('RelationAttachments', '')
        if relation_attachments:
            attachment_blocks = relation_attachments.split('\n\n')
            for block in attachment_blocks:
                name = ''
                checksum = ''
                for line in block.split('\n'):
                    if line.startswith('n='):
                        name = line[2:]
                    elif line.startswith('d='):
                        checksum = line[2:]
                
                if name and checksum:
                    print(f'$PROJECT\t$TRENCH\t{name}\t{checksum}')
except:
    pass
" >> attachments.txt
    fi
done

count=$(tail -n +2 attachments.txt | wc -l)
echo "Total attachments found: $count" >&2