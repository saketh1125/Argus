#!/usr/bin/env python3
"""
CodeSentinel Control Panel
--------------------------
Start / stop services, manage .env keys, and monitor status in real time.
"""

import json
import os
import signal
import subprocess
import sys
import threading
import time
import tkinter as tk
from pathlib import Path
from tkinter import ttk, messagebox, scrolledtext

# ── paths ──────────────────────────────────────────────────────────────
PROJECT_DIR = Path(__file__).resolve().parent
ENV_FILE = PROJECT_DIR / ".env"
ENV_EXAMPLE = PROJECT_DIR / ".env.example"
PID_FILE = PROJECT_DIR / ".codesentinel_pids.json"

# ── colour palette ─────────────────────────────────────────────────────
BG        = "#1e1e2e"
BG_CARD   = "#2a2a3d"
BG_ENTRY  = "#363652"
FG        = "#cdd6f4"
FG_DIM    = "#6c7086"
ACCENT    = "#89b4fa"
GREEN     = "#a6e3a1"
RED       = "#f38ba8"
YELLOW    = "#f9e2af"
ORANGE    = "#fab387"
SURFACE   = "#313244"
BORDER    = "#45475a"


# ── helpers ────────────────────────────────────────────────────────────
def load_env() -> dict[str, str]:
    """Parse .env into a dict (keys only, values hidden)."""
    env: dict[str, str] = {}
    if not ENV_FILE.exists():
        return env
    for line in ENV_FILE.read_text().splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        eq = line.find("=")
        if eq < 0:
            continue
        key = line[:eq].strip()
        val = line[eq + 1:].strip().strip("\"'")
        env[key] = val
    return env


def save_env(env: dict[str, str]) -> None:
    """Write the full .env back, preserving comments from example."""
    lines: list[str] = []
    if ENV_EXAMPLE.exists():
        for raw in ENV_EXAMPLE.read_text().splitlines():
            s = raw.strip()
            if not s or s.startswith("#"):
                lines.append(raw)
                continue
            eq = s.find("=")
            if eq < 0:
                lines.append(raw)
                continue
            key = s[:eq].strip()
            if key in env:
                lines.append(f"{key}={env[key]}")
            else:
                lines.append(raw)
        # add any extra keys not in example
        for k, v in env.items():
            if not any(l.strip().startswith(f"{k}=") for l in lines):
                lines.append(f"{k}={v}")
    else:
        for k, v in env.items():
            lines.append(f"{k}={v}")
    ENV_FILE.write_text("\n".join(lines) + "\n")


def _read_pids() -> dict[str, int]:
    if PID_FILE.exists():
        try:
            return json.loads(PID_FILE.read_text())
        except Exception:
            pass
    return {}


def _write_pids(data: dict[str, int]) -> None:
    PID_FILE.write_text(json.dumps(data))


def _save_pid(name: str, pid: int) -> None:
    pids = _read_pids()
    pids[name] = pid
    _write_pids(pids)


def _remove_pid(name: str) -> None:
    pids = _read_pids()
    pids.pop(name, None)
    _write_pids(pids)


def _is_alive(pid: int) -> bool:
    try:
        os.kill(pid, 0)
        return True
    except (OSError, ProcessLookupError):
        return False


def _kill_pid(pid: int) -> bool:
    try:
        os.kill(pid, signal.SIGTERM)
        time.sleep(0.3)
        if _is_alive(pid):
            os.kill(pid, signal.SIGKILL)
        return True
    except (OSError, ProcessLookupError):
        return False


# ── tkinter styling ────────────────────────────────────────────────────
def apply_theme(root: tk.Tk) -> ttk.Style:
    style = ttk.Style(root)
    style.theme_use("clam")

    style.configure(".", background=BG, foreground=FG, fieldbackground=BG_ENTRY,
                     borderwidth=0, focuscolor=ACCENT)
    style.map(".", background=[("active", SURFACE)])

    # ── frame ──
    style.configure("Card.TFrame", background=BG_CARD, relief="flat")
    style.configure("Main.TFrame", background=BG)
    style.configure("Log.TFrame", background=BG)

    # ── label ──
    style.configure("TLabel", background=BG, foreground=FG, font=("Segoe UI", 11))
    style.configure("Title.TLabel", font=("Segoe UI", 20, "bold"), foreground=FG,
                     background=BG)
    style.configure("Subtitle.TLabel", font=("Segoe UI", 10), foreground=FG_DIM,
                     background=BG)
    style.configure("Card.TLabel", background=BG_CARD, foreground=FG,
                     font=("Segoe UI", 11))
    style.configure("CardTitle.TLabel", background=BG_CARD, foreground=FG,
                     font=("Segoe UI", 12, "bold"))
    style.configure("Status.TLabel", background=BG_CARD, font=("Segoe UI", 11))
    style.configure("Running.TLabel", background=BG_CARD, foreground=GREEN,
                     font=("Segoe UI", 11, "bold"))
    style.configure("Stopped.TLabel", background=BG_CARD, foreground=RED,
                     font=("Segoe UI", 11, "bold"))
    style.configure("KeyLabel.TLabel", background=BG, foreground=FG_DIM,
                     font=("Consolas", 10))
    style.configure("KeyVal.TLabel", background=BG, foreground=GREEN,
                     font=("Consolas", 10, "bold"))
    style.configure("Log.TLabel", background=BG, foreground=FG_DIM,
                     font=("Consolas", 9))

    # ── button ──
    style.configure("TButton", font=("Segoe UI", 10, "bold"), padding=(14, 6),
                     background=ACCENT, foreground=BG, borderwidth=0)
    style.map("TButton",
              background=[("active", "#74a8f7"), ("disabled", SURFACE)],
              foreground=[("disabled", FG_DIM)])

    style.configure("Start.TButton", background=GREEN, foreground=BG)
    style.map("Start.TButton", background=[("active", "#86d48b"), ("disabled", SURFACE)])

    style.configure("Stop.TButton", background=RED, foreground=BG)
    style.map("Stop.TButton", background=[("active", "#e07080"), ("disabled", SURFACE)])

    style.configure("Open.TButton", background=YELLOW, foreground=BG)
    style.map("Open.TButton", background=[("active", "#e8d090")])

    style.configure("Accent.TButton", background=ACCENT, foreground=BG)
    style.map("Accent.TButton", background=[("active", "#74a8f7")])

    # ── entry ──
    style.configure("TEntry", fieldbackground=BG_ENTRY, foreground=FG,
                     insertcolor=FG, borderwidth=1, padding=4)
    style.map("TEntry", fieldbackground=[("focus", "#404060")])

    # ── notebook (tabs) ──
    style.configure("TNotebook", background=BG, borderwidth=0)
    style.configure("TNotebook.Tab", background=SURFACE, foreground=FG_DIM,
                     padding=(16, 8), font=("Segoe UI", 10, "bold"))
    style.map("TNotebook.Tab",
              background=[("selected", BG_CARD)],
              foreground=[("selected", ACCENT)])

    # ── scrollbar ──
    style.configure("Vertical.TScrollbar", background=SURFACE, troughcolor=BG,
                     borderwidth=0, arrowcolor=FG_DIM)

    return style


# ── main application ───────────────────────────────────────────────────
class ControlPanel(tk.Tk):
    def __init__(self) -> None:
        super().__init__()
        self.title("CodeSentinel — Control Panel")
        self.geometry("820x720")
        self.minsize(720, 600)
        self.configure(bg=BG)
        self.resizable(True, True)
        apply_theme(self)

        self._running: dict[str, subprocess.Popen | None] = {
            "qdrant": None,
            "ollama": None,
            "codesentinel": None,
        }
        self._env_data = load_env()

        self._build_ui()
        self._poll_status()
        self._log("Control panel ready.")

    # ── UI construction ───────────────────────────────────────────────
    def _build_ui(self) -> None:
        # header
        hdr = ttk.Frame(self, style="Main.TFrame")
        hdr.pack(fill="x", padx=24, pady=(18, 2))
        ttk.Label(hdr, text="CodeSentinel", style="Title.TLabel").pack(side="left")
        ttk.Label(hdr, text="v1.0  •  Control Panel", style="Subtitle.TLabel"
                  ).pack(side="left", padx=(12, 0), pady=(6, 0))

        # tabs
        self._nb = ttk.Notebook(self)
        self._nb.pack(fill="both", expand=True, padx=16, pady=(10, 16))

        self._build_services_tab()
        self._build_keys_tab()

    # ── services tab ──────────────────────────────────────────────────
    def _build_services_tab(self) -> None:
        tab = ttk.Frame(self._nb, style="Card.TFrame")
        self._nb.add(tab, text="  Services  ")

        self._svc_frames: dict[str, ttk.Frame] = {}
        self._svc_status_labels: dict[str, ttk.Label] = {}
        self._svc_status_vars: dict[str, tk.StringVar] = {}

        services = [
            ("qdrant",          "Qdrant Vector DB",  "docker run -d -p 6333:6333 qdrant/qdrant"),
            ("ollama",          "Ollama Embeddings",  "ollama serve"),
            ("codesentinel",    "CodeSentinel",       None),
        ]

        for i, (name, display, cmd) in enumerate(services):
            card = ttk.Frame(tab, style="Card.TFrame")
            card.pack(fill="x", padx=14, pady=8, ipady=8, ipadx=8)
            self._svc_frames[name] = card

            # title row
            top = ttk.Frame(card, style="Card.TFrame")
            top.pack(fill="x", padx=12, pady=(6, 2))

            ttk.Label(top, text=display, style="CardTitle.TLabel").pack(side="left")

            status_var = tk.StringVar(value="Checking...")
            self._svc_status_vars[name] = status_var
            lbl = ttk.Label(top, textvariable=status_var, style="Status.TLabel")
            lbl.pack(side="right")
            self._svc_status_labels[name] = lbl

            # buttons row
            btns = ttk.Frame(card, style="Card.TFrame")
            btns.pack(fill="x", padx=12, pady=(4, 6))

            start_btn = ttk.Button(btns, text="Start", style="Start.TButton",
                                   command=lambda n=name: self._start_service(n))
            start_btn.pack(side="left", padx=(0, 8))

            stop_btn = ttk.Button(btns, text="Stop", style="Stop.TButton",
                                  command=lambda n=name: self._stop_service(n))
            stop_btn.pack(side="left")

            if name == "codesentinel":
                # extra: open dashboard button
                ttk.Button(btns, text="Open Dashboard", style="Open.TButton",
                           command=self._open_dashboard).pack(side="right")
                # repo entry
                ttk.Label(btns, text="Repo:", style="Card.TLabel").pack(
                    side="left", padx=(20, 4))
                self._repo_var = tk.StringVar(value="./testdata/sample-repo")
                ttk.Entry(btns, textvariable=self._repo_var, width=36,
                          style="TEntry").pack(side="left")

        # separator
        ttk.Separator(tab, orient="horizontal").pack(fill="x", padx=14, pady=4)

        # log area
        log_frame = ttk.Frame(tab, style="Log.TFrame")
        log_frame.pack(fill="both", expand=True, padx=14, pady=(4, 8))
        ttk.Label(log_frame, text="Activity Log", style="Subtitle.TLabel"
                  ).pack(anchor="w", padx=4, pady=(0, 4))

        self._log_text = scrolledtext.ScrolledText(
            log_frame, wrap="word", height=12,
            bg=BG_ENTRY, fg=FG_DIM, insertbackground=FG,
            font=("Consolas", 9), relief="flat", borderwidth=0,
            highlightthickness=1, highlightbackground=BORDER,
            highlightcolor=BORDER,
        )
        self._log_text.pack(fill="both", expand=True)
        self._log_text.configure(state="disabled")

    # ── keys tab ──────────────────────────────────────────────────────
    def _build_keys_tab(self) -> None:
        tab = ttk.Frame(self._nb, style="Card.TFrame")
        self._nb.add(tab, text="  API Keys  ")

        ttk.Label(tab, text="Edit .env credentials", style="CardTitle.TLabel"
                  ).pack(anchor="w", padx=16, pady=(14, 2))
        ttk.Label(tab, text="Leave blank for mock mode  •  Changes saved on Apply",
                  style="Subtitle.TLabel").pack(anchor="w", padx=16, pady=(0, 10))

        key_defs = [
            ("GITHUB_TOKEN",  "GitHub PAT (PR creation)"),
            ("NIM_API_KEY",   "Nvidia NIM (LLM)"),
            ("NIM_BASE_URL",  "NIM Base URL"),
            ("NIM_MODEL",     "NIM Model"),
            ("JINA_API_KEY",  "Jina AI (embeddings)"),
            ("GEMINI_API_KEY","Gemini (embeddings)"),
            ("E2B_API_KEY",   "E2B (code sandbox)"),
            ("OLLAMA_HOST",   "Ollama host"),
            ("QDRANT_HOST",   "Qdrant host"),
            ("QDRANT_PORT",   "Qdrant port"),
            ("EMBED_MODEL",   "Embed model"),
        ]

        self._key_entries: dict[str, ttk.Entry] = {}

        canvas = tk.Canvas(tab, bg=BG_CARD, highlightthickness=0)
        scrollbar = ttk.Scrollbar(tab, orient="vertical", command=canvas.yview)
        inner = ttk.Frame(canvas, style="Card.TFrame")

        inner.bind("<Configure>", lambda e: canvas.configure(scrollregion=canvas.bbox("all")))
        canvas.create_window((0, 0), window=inner, anchor="nw")
        canvas.configure(yscrollcommand=scrollbar.set)

        canvas.pack(side="left", fill="both", expand=True, padx=(14, 0), pady=8)
        scrollbar.pack(side="right", fill="y", padx=(0, 14), pady=8)

        for key, label in key_defs:
            row = ttk.Frame(inner, style="Card.TFrame")
            row.pack(fill="x", padx=12, pady=3)

            ttk.Label(row, text=label, style="KeyLabel.TLabel", width=28,
                      anchor="w").pack(side="left")
            var = tk.StringVar(value=self._env_data.get(key, ""))
            entry = ttk.Entry(row, textvariable=var, width=44, show="•",
                              style="TEntry")
            entry.pack(side="left", padx=(8, 0))

            # toggle visibility
            vis_var = tk.BooleanVar(value=False)
            def _toggle(e=entry, v=var, vis=vis_var, key=key):
                if vis.get():
                    e.configure(show="•")
                    vis.set(False)
                else:
                    e.configure(show="")
                    vis.set(True)
            btn = ttk.Button(row, text="👁", width=3, command=_toggle)
            btn.pack(side="left", padx=(4, 0))

            self._key_entries[key] = entry

        # apply button
        bar = ttk.Frame(tab, style="Card.TFrame")
        bar.pack(fill="x", padx=14, pady=(0, 10))
        ttk.Button(bar, text="Apply & Save", style="Accent.TButton",
                   command=self._apply_keys).pack(side="right")
        ttk.Button(bar, text="Reload", style="TButton",
                   command=self._reload_keys).pack(side="right", padx=(0, 8))

    # ── service control ───────────────────────────────────────────────
    def _start_service(self, name: str) -> None:
        if self._running.get(name) and self._running[name] is not None:
            self._log(f"[{name}] already running (PID {self._running[name].pid})")
            return

        if name == "qdrant":
            self._log("[qdrant] Starting Docker container...")
            try:
                # check if container already exists
                check = subprocess.run(
                    ["docker", "ps", "-a", "-q", "-f", "ancestor=qdrant/qdrant"],
                    capture_output=True, text=True, timeout=5)
                if check.stdout.strip():
                    container_id = check.stdout.strip().split("\n")[0]
                    subprocess.run(["docker", "start", container_id],
                                   capture_output=True, timeout=10)
                    self._log(f"[qdrant] Started existing container {container_id[:12]}")
                else:
                    result = subprocess.run(
                        ["docker", "run", "-d", "-p", "6333:6333", "qdrant/qdrant"],
                        capture_output=True, text=True, timeout=30)
                    if result.returncode == 0:
                        cid = result.stdout.strip()[:12]
                        self._log(f"[qdrant] Container started: {cid}")
                    else:
                        self._log(f"[qdrant] ERROR: {result.stderr.strip()}")
                        return
            except FileNotFoundError:
                self._log("[qdrant] ERROR: docker not found. Install Docker first.")
                return
            except subprocess.TimeoutExpired:
                self._log("[qdrant] ERROR: Docker command timed out.")
                return
            self._poll_status()
            return

        if name == "ollama":
            self._log("[ollama] Starting ollama serve...")
            try:
                proc = subprocess.Popen(
                    ["ollama", "serve"],
                    stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                    start_new_session=True)
                _save_pid("ollama", proc.pid)
                self._running["ollama"] = proc
                self._log(f"[ollama] Started (PID {proc.pid})")
            except FileNotFoundError:
                self._log("[ollama] ERROR: ollama not found. Install Ollama first.")
                return
            self._poll_status()
            return

        if name == "codesentinel":
            repo = self._repo_var.get().strip() or "./testdata/sample-repo"
            self._log(f"[codesentinel] Starting pipeline for {repo}...")
            try:
                env = os.environ.copy()
                env.update(self._env_data)
                proc = subprocess.Popen(
                    [sys.executable, "-m", "go"],  # fallback
                    cwd=str(PROJECT_DIR), env=env,
                    stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                    start_new_session=True)
            except Exception:
                pass

            # prefer `go run .` directly
            try:
                env = os.environ.copy()
                env.update(self._env_data)
                proc = subprocess.Popen(
                    ["go", "run", ".", "--repo", repo, "--web"],
                    cwd=str(PROJECT_DIR), env=env,
                    stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                    start_new_session=True, text=True)
                _save_pid("codesentinel", proc.pid)
                self._running["codesentinel"] = proc
                self._log(f"[codesentinel] Started (PID {proc.pid})")
                # stream output in background
                threading.Thread(target=self._stream_output, args=(proc,),
                                 daemon=True).start()
            except FileNotFoundError:
                self._log("[codesentinel] ERROR: go not found. Install Go first.")
                return
            self._poll_status()
            return

    def _stop_service(self, name: str) -> None:
        if name == "qdrant":
            self._log("[qdrant] Stopping Docker container...")
            try:
                check = subprocess.run(
                    ["docker", "ps", "-q", "-f", "ancestor=qdrant/qdrant"],
                    capture_output=True, text=True, timeout=5)
                if check.stdout.strip():
                    for cid in check.stdout.strip().split("\n"):
                        subprocess.run(["docker", "stop", cid], capture_output=True,
                                       timeout=10)
                        self._log(f"[qdrant] Stopped container {cid[:12]}")
                else:
                    self._log("[qdrant] No running container found.")
            except Exception as e:
                self._log(f"[qdrant] ERROR stopping: {e}")
            self._poll_status()
            return

        pid = _read_pids().get(name)
        if pid and _is_alive(pid):
            self._log(f"[{name}] Stopping PID {pid}...")
            _kill_pid(pid)
            _remove_pid(name)
            self._running[name] = None
            self._log(f"[{name}] Stopped.")
        else:
            self._log(f"[{name}] Not running.")
            _remove_pid(name)
            self._running[name] = None
        self._poll_status()

    def _open_dashboard(self) -> None:
        import webbrowser
        port = self._env_data.get("DASHBOARD_PORT", "8080")
        url = f"http://127.0.0.1:{port}"
        self._log(f"[dashboard] Opening {url}")
        webbrowser.open(url)

    def _stream_output(self, proc: subprocess.Popen) -> None:
        try:
            for line in proc.stdout:
                if line:
                    self.after(0, self._log, f"[codesentinel] {line.rstrip()}")
        except Exception:
            pass

    # ── key management ────────────────────────────────────────────────
    def _apply_keys(self) -> None:
        for key, entry in self._key_entries.items():
            val = entry.get()
            if val == "•" * len(val):
                continue  # unchanged masked value
            self._env_data[key] = val
        # remove empty keys
        self._env_data = {k: v for k, v in self._env_data.items() if v}
        save_env(self._env_data)
        self._log("[env] .env saved.")

    def _reload_keys(self) -> None:
        self._env_data = load_env()
        for key, entry in self._key_entries.items():
            entry.configure(show="")
            entry.delete(0, "end")
            entry.insert(0, self._env_data.get(key, ""))
            entry.configure(show="•")
        self._log("[env] Reloaded from .env")

    # ── status polling ────────────────────────────────────────────────
    def _poll_status(self) -> None:
        threading.Thread(target=self._check_statuses, daemon=True).start()

    def _check_statuses(self) -> None:
        statuses: dict[str, tuple[str, str]] = {}

        # qdrant
        try:
            r = subprocess.run(
                ["docker", "ps", "-q", "-f", "ancestor=qdrant/qdrant"],
                capture_output=True, text=True, timeout=3)
            if r.stdout.strip():
                statuses["qdrant"] = ("Running", GREEN)
            else:
                statuses["qdrant"] = ("Stopped", RED)
        except Exception:
            statuses["qdrant"] = ("Unknown", YELLOW)

        # ollama
        ollama_pid = _read_pids().get("ollama")
        if ollama_pid and _is_alive(ollama_pid):
            statuses["ollama"] = ("Running", GREEN)
        else:
            # also check if it's listening on default port
            try:
                import urllib.request
                req = urllib.request.Request("http://127.0.0.1:11434/api/tags",
                                            method="GET")
                urllib.request.urlopen(req, timeout=2)
                statuses["ollama"] = ("Running (external)", GREEN)
            except Exception:
                statuses["ollama"] = ("Stopped", RED)

        # codesentinel
        cs_pid = _read_pids().get("codesentinel")
        if cs_pid and _is_alive(cs_pid):
            statuses["codesentinel"] = ("Running", GREEN)
        else:
            statuses["codesentinel"] = ("Stopped", RED)

        # update UI on main thread
        self.after(0, self._update_status_labels, statuses)
        self.after(3000, self._poll_status)

    def _update_status_labels(self, statuses: dict[str, tuple[str, str]]) -> None:
        for name, (text, color) in statuses.items():
            var = self._svc_status_vars.get(name)
            lbl = self._svc_status_labels.get(name)
            if var:
                var.set(text)
            if lbl:
                if color == GREEN:
                    lbl.configure(style="Running.TLabel")
                elif color == RED:
                    lbl.configure(style="Stopped.TLabel")
                else:
                    lbl.configure(style="Status.TLabel")

    # ── logging ───────────────────────────────────────────────────────
    def _log(self, msg: str) -> None:
        ts = time.strftime("%H:%M:%S")
        line = f"[{ts}] {msg}\n"
        self._log_text.configure(state="normal")
        self._log_text.insert("end", line)
        self._log_text.see("end")
        self._log_text.configure(state="disabled")


# ── entry point ────────────────────────────────────────────────────────
if __name__ == "__main__":
    app = ControlPanel()
    app.mainloop()
