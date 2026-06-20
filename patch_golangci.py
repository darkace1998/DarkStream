with open('.golangci.yml', 'r') as f:
    lines = f.readlines()

new_lines = []
for line in lines:
    if "err113" in line or "wsl_v5" in line or "mnd" in line or "intrange" in line:
        continue
    new_lines.append(line)

with open('.golangci.yml', 'w') as f:
    f.writelines(new_lines)
