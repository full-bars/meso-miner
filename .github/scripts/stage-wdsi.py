#!/usr/bin/env python3
"""
stage-wdsi.py — Automate Microsoft WDSI false-positive submission preparation.

Runs as a post-scan step in CI after vt-scan.py. For each artifact with
malicious > 0, it:
  1. Queries the VT API for Microsoft's detection name + engine version
  2. Copies the local binary (from the already-downloaded scan artifacts)
  3. Verifies SHA256 against the VT record
  4. Uses kilo-free (via zenproxy) to generate a contextual WDSI submission
     text block (no hardcoded detection names — adapts to whatever Microsoft
     actually flags)
  5. Bundles everything into defender-submissions-<tag>/ with WDSI-submission-text.txt

Usage:
  python3 stage-wdsi.py <release-tag> <vt-results-json> <source-dir>

Env:
  VIRUS_TOTAL  — VT API key
  ZENPROXY_KEY — Optional; if unset, uses the public zenproxy endpoint

Output:
  defender-submissions-<tag>/
    <flagged binaries...>
    WDSI-submission-text.txt
"""
import hashlib
import json
import os
import shutil
import sys
import urllib.request

VT_API = "https://www.virustotal.com/api/v3"
ZENPROXY_URL = os.environ.get("ZENPROXY_URL", "https://ohmyproxy.12388321.xyz/v1")


def vt_api(path, api_key):
    """Make an authenticated request to the VirusTotal v3 API."""
    req = urllib.request.Request(VT_API + path)
    req.add_header("x-apikey", api_key)
    with urllib.request.urlopen(req, timeout=60) as resp:
        return json.loads(resp.read())


def sha256_of(path):
    """Compute the SHA256 hex digest of a file."""
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def zenproxy_chat(messages, api_key=None):
    """Call kilo-free via zenproxy to generate WDSI submission text."""
    payload = {
        "model": "kilo-free",
        "messages": messages,
        "max_tokens": 2048,
        "temperature": 0.3,
    }
    data = json.dumps(payload).encode()
    req = urllib.request.Request(f"{ZENPROXY_URL}/chat/completions", data=data)
    req.add_header("Content-Type", "application/json")
    if api_key:
        req.add_header("Authorization", f"Bearer {api_key}")
    with urllib.request.urlopen(req, timeout=120) as resp:
        result = json.loads(resp.read())
    return result["choices"][0]["message"]["content"]


def scan_path_to_asset(path):
    """Map a VT scan path (e.g. release_tmp/linux/amd64/provider) to a
    human-readable artifact name for the WDSI form and the destination dir."""
    basename = os.path.basename(path)
    # Build a descriptive name with platform
    parts = path.split(os.sep)
    platform = ""
    if "darwin" in parts:
        platform = "macOS"
    elif "linux" in parts:
        platform = "Linux"
    elif "windows" in parts:
        platform = "Windows"
    elif "amd64" in parts or "arm64" in parts:
        # hub_tmp/amd64/hub and hub_tmp/arm64/hub — infer Linux
        platform = "Linux"
    arch = ""
    if "arm64" in parts:
        arch = "arm64"
    elif "amd64" in parts:
        arch = "amd64"

    # Special names for display
    if basename == "provider":
        display = "urnetwork-provider"
    elif basename == "hub":
        display = "urnetwork-hub"
    elif basename == "urnet-tools":
        display = "urnet-tools"
    elif basename == "urnet-docker":
        display = "urnet-docker"
    else:
        display = basename

    ext = ".exe" if platform == "Windows" else ""
    return f"{display}-{platform.lower()}-{arch}{ext}", basename, platform, arch


def main():
    """Stage flagged binaries and WDSI submission text for a release tag."""
    if len(sys.argv) < 4:
        print("usage: stage-wdsi.py <release-tag> <vt-results-json> <source-dir>", file=sys.stderr)
        return 2

    tag = sys.argv[1]
    vt_json_path = sys.argv[2]
    source_dir = sys.argv[3]  # e.g. release_tmp/ — already downloaded by scan job

    vt_key = os.environ.get("VIRUS_TOTAL", "")
    zenproxy_key = os.environ.get("ZENPROXY_KEY", "")

    if not vt_key:
        print("ERROR: VIRUS_TOTAL not set", file=sys.stderr)
        return 2

    # Read VT scan results
    with open(vt_json_path) as f:
        vt_results = json.load(f)

    # Filter to files with Microsoft detections (malicious > 0)
    flagged = [r for r in vt_results if (r.get("malicious") or 0) > 0]
    if not flagged:
        print("No flagged files — nothing to stage")
        return 0

    dest_dir = f"defender-submissions-{tag.replace('v', '', 1)}"
    if os.path.exists(dest_dir):
        shutil.rmtree(dest_dir)
    os.makedirs(dest_dir)

    # First pass: gather detection details from VT for each flagged file
    file_details = []
    for row in flagged:
        sha = row["sha"]
        scan_path = row["path"]
        mal = row["malicious"]
        asset_name, basename, platform, arch = scan_path_to_asset(scan_path)

        # Query VT for engine-specific details
        detection_name = "Unknown"
        engine_version = ""
        try:
            result = vt_api(f"/files/{sha}", vt_key)
            last_results = result.get("data", {}).get("attributes", {}).get("last_analysis_results", {})
            msft = last_results.get("Microsoft", {})
            detection_name = msft.get("result", "Unknown")
            engine_version = msft.get("engine_version", "")
        except Exception as e:
            print(f"  WARNING: VT lookup for {sha} failed: {e}", file=sys.stderr)

        # Find the local file — scan job organizes by tool category
        local_path = None
        basename_scan = scan_path  # e.g. release_tmp/linux/amd64/provider
        # Try the exact path first (release_tmp subdirs)
        candidate = os.path.join(source_dir, basename_scan)
        if not os.path.isfile(candidate):
            # Try just the basename in source_dir
            candidate = os.path.join(source_dir, os.path.basename(basename_scan))
        if os.path.isfile(candidate):
            local_path = candidate
        else:
            # Hubs and tools are in hub_tmp/ and tool_tmp/ respectively
            parts = basename_scan.split(os.sep)
            plat_parts = [p for p in parts if p in ("linux", "darwin", "windows", "amd64", "arm64")]
            file_base = os.path.basename(basename_scan)
            for prefix in ["hub_tmp", "tool_tmp"]:
                sub = os.path.join(prefix, *plat_parts) if plat_parts else prefix
                candidate2 = os.path.join(source_dir, sub, file_base)
                if os.path.isfile(candidate2):
                    local_path = candidate2
                    break
            if local_path is None:
                print(f"  WARNING: local file not found for {scan_path}", file=sys.stderr)

        file_details.append({
            "sha": sha,
            "malicious": mal,
            "detection_name": detection_name,
            "engine_version": engine_version,
            "asset_name": asset_name,
            "basename": basename,
            "platform": platform,
            "arch": arch,
            "local_path": local_path,
        })
        print(f"  {asset_name}: {detection_name} (engine: {engine_version}, SHA: {sha[:16]}...)")

    # Second pass: copy flagged binaries from local source, filter out failures
    verified_details = []
    for fd in file_details:
        if not fd["local_path"]:
            print(f"  SKIP: no local file for {fd['asset_name']}", file=sys.stderr)
            continue

        dest_file = os.path.join(dest_dir, fd["asset_name"])
        shutil.copy2(fd["local_path"], dest_file)
        os.chmod(dest_file, 0o755)

        # Verify SHA256
        actual_sha = sha256_of(dest_file)
        if actual_sha != fd["sha"]:
            print(f"  SHA MISMATCH for {fd['asset_name']}! expected {fd['sha']}, got {actual_sha}", file=sys.stderr)
            os.remove(dest_file)
            continue
        print(f"  Copied {fd['asset_name']} (SHA256 verified)")
        verified_details.append(fd)
    file_details = verified_details

    # Third pass: generate WDSI submission text via kilo-free
    print("  Generating WDSI submission text via kilo-free...")
    prompt = [
        {
            "role": "system",
            "content": (
                "You are preparing Microsoft WDSI false-positive submission text for "
                "open-source Go binaries. Given a list of flagged files with their "
                "Microsoft detection name, engine version, SHA256, platform, and "
                "architecture, generate a pre-filled WDSI submission document.\n\n"
                "Include:\n"
                "1. A header with the detection name, engine version, and product "
                "(Windows Defender)\n"
                "2. For each file: file name, SHA256, VT report URL, malicious count, "
                "detection name, and an 'Additional info' paragraph that explains:\n"
                "   - It is an open-source Go binary (MPL-2.0 license) from "
                "https://github.com/full-bars/urnetwork-3.23-fix\n"
                "   - Built with -trimpath -s -w (stripped, which is why ML heuristics "
                "flag it)\n"
                "   - The detection is a known machine-learning false positive on "
                "stripped Go binaries\n"
                "   - The specific platform/architecture\n"
                "3. A note about one-file-per-submission and the WDSI form URL\n\n"
                "Do NOT use markdown headers or bullet points that won't render in a "
                "text form. Use plain sections separated by '---'. Keep it concise."
            ),
        },
        {
            "role": "user",
            "content": (
                f"Generate WDSI submission text for {len(file_details)} files flagged in "
                f"release {tag}:\n\n"
                + "\n".join(
                    f"- {fd['asset_name']} ({fd['platform']} {fd['arch']}): "
                    f"detection={fd['detection_name']}, engine={fd['engine_version']}, "
                    f"SHA256={fd['sha']}, malicious={fd['malicious']}, "
                    f"VT=https://www.virustotal.com/gui/file/{fd['sha']}"
                    for fd in file_details
                )
            ),
        },
    ]

    try:
        wdsi_text = zenproxy_chat(prompt, zenproxy_key)
    except Exception as e:
        print(f"  WARNING: zenproxy call failed: {e} — writing fallback template", file=sys.stderr)
        wdsi_text = None

    if wdsi_text:
        with open(os.path.join(dest_dir, "WDSI-submission-text.txt"), "w") as f:
            f.write(wdsi_text)
    else:
        # Fallback: write a basic template
        lines = [
            f"=== Microsoft WDSI False-Positive Submission — {tag} ===",
            "",
            "Submission type: My file was incorrectly detected",
            "Product: Windows Defender",
        ]
        for fd in file_details:
            lines.extend([
                "---",
                "",
                f"File: {fd['asset_name']}",
                f"SHA256: {fd['sha']}",
                f"Malicious: {fd['malicious']}",
                f"VT: https://www.virustotal.com/gui/file/{fd['sha']}",
                f"Detection: {fd['detection_name']}",
                f"Engine: {fd['engine_version']}",
                "",
                "Additional info:",
                f"Open-source Go binary (MPL-2.0) from "
                f"https://github.com/full-bars/urnetwork-3.23-fix — "
                f"{fd['asset_name']} ({fd['platform']} {fd['arch']}). "
                f"Built with -trimpath -s -w. {fd['detection_name']} is a known "
                f"ML false positive on stripped Go binaries.",
                "",
            ])
        lines.extend([
            "---",
            "",
            "WDSI submission form: https://www.microsoft.com/en-us/wdsi/filesubmission",
            "One file per submission.",
            f"Bundle location: {dest_dir}/",
        ])
        with open(os.path.join(dest_dir, "WDSI-submission-text.txt"), "w") as f:
            f.write("\n".join(lines) + "\n")

    print(f"\nDone. Staged {len(file_details)} files in {dest_dir}/")
    print(f"WDSI text: {dest_dir}/WDSI-submission-text.txt")


if __name__ == "__main__":
    sys.exit(main())
