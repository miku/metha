import pandas as pd

pd.set_option('display.max_rows', None)

df = pd.read_json("sites.json", lines=True)
print(df.groupby("tld").size().sort_values(ascending=False).head(25).to_markdown())

print()

tep = df.groupby(["tld", "is_edu", "platform"]).size()
print(tep.to_markdown())
