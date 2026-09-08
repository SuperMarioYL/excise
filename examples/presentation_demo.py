import hashlib, pathlib, subprocess
path = pathlib.Path("testdata/claude_session_simple.jsonl")
before = hashlib.sha256(path.read_bytes()).hexdigest()
result = subprocess.run(["go", "run", "./cmd/excise", "cut", "5", str(path), "--dry-run"], text=True, capture_output=True, check=True)
print(result.stdout.strip())
print(result.stderr.strip())
print("source unchanged:", hashlib.sha256(path.read_bytes()).hexdigest() == before)
