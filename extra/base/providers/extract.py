#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.12"
# dependencies = ["bs4"]
# ///

import json
import re
from bs4 import BeautifulSoup
import sys

def clean_text(text):
    """Clean and normalize text content."""
    if not text:
        return ""
    return re.sub(r'\s+', ' ', text.strip())

def extract_field_value(text, field_name):
    """Extract value after field name from text."""
    pattern = rf'{re.escape(field_name)}\s*(.+?)(?=\n|$)'
    match = re.search(pattern, text, re.IGNORECASE)
    return clean_text(match.group(1)) if match else ""

def parse_document_info(text):
    """Parse document count and open access information."""
    doc_pattern = r'Number of documents:\s*(\d+(?:[.,]\d+)*)'
    oa_pattern = r'davon Open Access[^:]*:\s*([^(]+(?:\([^)]+\))?)'

    doc_match = re.search(doc_pattern, text)
    oa_match = re.search(oa_pattern, text)

    doc_count = clean_text(doc_match.group(1)) if doc_match else ""
    oa_info = clean_text(oa_match.group(1)) if oa_match else ""

    return doc_count, oa_info

def extract_provider_data(row):
    """Extract all data for a single content provider."""
    provider = {}

    # Extract provider name
    name_elem = row.find('div', class_='ContentProvider')
    if name_elem:
        # Get text content, excluding nested elements like links
        name_text = name_elem.get_text()
        # Remove the question mark link text
        provider['name'] = clean_text(name_text.split('Further information')[0])

    # Find the details list
    details_list = row.find('ul', class_='TabSourcesUl')
    if not details_list:
        return provider

    # Extract all detail text
    details_text = details_list.get_text('\n')

    # Extract individual fields
    provider['url'] = extract_field_value(details_text, 'URL:')
    provider['continent'] = extract_field_value(details_text, 'Continent:')
    provider['country'] = extract_field_value(details_text, 'Country:')

    # Parse document information
    doc_count, oa_info = parse_document_info(details_text)
    provider['document_count'] = doc_count
    provider['open_access_info'] = oa_info

    provider['type'] = extract_field_value(details_text, 'Type:')
    provider['system'] = extract_field_value(details_text, 'System:')
    provider['in_base_since'] = extract_field_value(details_text, 'In BASE since:')
    provider['base_url'] = extract_field_value(details_text, 'BASE URL:')

    # Optional fields
    ror_match = re.search(r'https://ror\.org/[^\s]+', details_text)
    if ror_match:
        provider['ror'] = ror_match.group(0)

    coords = extract_field_value(details_text, 'Latitude/Longitude:')
    if coords:
        provider['coordinates'] = coords

    # Clean up empty fields
    return {k: v for k, v in provider.items() if v}

def main():
    if len(sys.argv) != 2:
        print("Usage: python3 script.py <html_file>", file=sys.stderr)
        sys.exit(1)

    html_file = sys.argv[1]

    try:
        with open(html_file, 'r', encoding='utf-8') as f:
            html_content = f.read()
    except FileNotFoundError:
        print(f"Error: File '{html_file}' not found", file=sys.stderr)
        sys.exit(1)

    soup = BeautifulSoup(html_content, 'html.parser')

    # Find all content provider rows
    provider_rows = soup.find_all('tr', class_='border-top-grau')

    providers = []
    for row in provider_rows:
        # Check if this row contains a content provider
        if row.find('div', class_='ContentProvider'):
            provider_data = extract_provider_data(row)
            if provider_data.get('name'):  # Only add if we got a name
                providers.append(provider_data)

    # Output JSON
    print(json.dumps(providers, indent=2, ensure_ascii=False))

if __name__ == "__main__":
    main()
