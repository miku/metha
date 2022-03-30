import fileinput
import requests

user_agent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/100.0.4896.60 Safari/537.36"

for i, line in enumerate(fileinput.input()):
    line = line.strip()
    if not line or not line.startswith("http"):
        continue
    try:
        resp = requests.get(
            line,
            verify=False,
            timeout=10,
            headers={
                "User-Agent": user_agent,
            },
        )
    except (
        requests.exceptions.ConnectionError,
        requests.exceptions.ReadTimeout,
    ) as exc:
        print("{}\t{}\tNA".format(i, line))
    else:
        print("{}\t{}\t{}".format(i, line, resp.url))
