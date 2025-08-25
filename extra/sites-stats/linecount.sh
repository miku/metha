#!/bin/bash

file="../../contrib/sites.tsv"



git log --follow --format=%H:%ci -- "$file" | while IFS= read -r line; do
    commit_hash=$(echo "$line" | cut -d':' -f1)
    commit_date=$(echo "$line" | cut -d':' -f2-)
    git checkout "$commit_hash" -- "$file" >/dev/null 2>&1
    line_count=$(wc -l < "$file")
    echo "$commit_date: $line_count"
done

