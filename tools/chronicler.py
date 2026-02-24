import os
import json
import shutil
import re
import sys
import logging
from pathlib import Path

# --- Configuration & Logging Setup ---
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s [%(levelname)s] %(message)s',
    datefmt='%H:%M:%S'
)
logger = logging.getLogger("Chronicler")

CHATS_DIR = Path("/workspace/chats").resolve()
MD_DIR = CHATS_DIR / "md"
SEARCH_ROOTS = [Path("/home/tazpod").resolve(), Path("/workspace").resolve()]

# Costanti di Protezione
MAX_JSON_SIZE_MB = 50
MIN_MD_CHAR_COUNT = 100  
MAX_BLOCK_LEN = 5000     

# IGNORE_PATTERNS: Solo cartelle che causerebbero loop infiniti o rumore temporaneo certo
IGNORE_PATTERNS = [
    "/workspace/chats/md", # Escludiamo solo i risultati Markdown per non ri-leggerli
    "/.gemini/tmp"
]

# Eccezioni: file che vogliamo tenere anche se contengono parole chiave del protocollo
KEEP_EXCEPTIONS = [
    "checkpoint-mnemosyne-bulk-upload.json",
    "session-2026-02-20T09-14-fee061f7.json"
]

def clean_text(text):
    """Pulisce il testo da caratteri non stampabili per un Markdown pulito e robusto."""
    if not isinstance(text, str): return str(text)
    return "".join(ch for ch in text if ch.isprintable() or ord(ch) in (10, 9))

def smart_truncate(text, max_len=MAX_BLOCK_LEN):
    """Tronca il testo in modo intelligente se troppo lungo, aggiungendo un marker."""
    if not text or len(text) <= max_len:
        return text
    
    half = max_len // 2
    return text[:half] + f"\n\n... [TRONCAMENTO CHIRURGICO: rimosse {len(text) - max_len} battute di log/rumore] ...\n\n" + text[-half:]

def extract_text_content(content):
    """Estrae il testo da 'content', che può essere stringa o lista di oggetti."""
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        texts = []
        for item in content:
            if isinstance(item, str):
                texts.append(item)
            elif isinstance(item, dict):
                texts.append(item.get("text", ""))
        return "".join(texts)
    return ""

def clean_protocol_noise(text):
    """Rimuove chirurgicamente i blocchi di protocollo e contenuti ricorsivi (meta-knowledge)."""
    if not text: return ""
    lines = text.splitlines()
    cleaned_lines = []
    
    noise_keywords = [
        "TAZLAB KNOWLEDGE EXTRACTION PROTOCOL",
        "Chief Archivist",
        "extract \"High-Resolution\" technical chronicles",
        "SESSION LOG BELOW",
        "--- Content from referenced files ---",
        "--- End of content ---",
        "This is the Gemini CLI. We are setting up the context",
        "Here is the folder structure of the current working directories",
        "Showing up to 200 items",
        "# Session: ",
        "### 👤 USER [",
        "### 🤖 ASSISTANT [",
        "**🛠️ Call: `",
        "**📦 Tool `",
        "[TRONCAMENTO CHIRURGICO:"
    ]
    
    for line in lines:
        if any(kw in line for kw in noise_keywords):
            continue
        cleaned_lines.append(line)
    
    return "\n".join(cleaned_lines).strip()

def is_meta_session(data, filename):
    """Controlla se la sessione è un lavoro di archiviazione (meta-sessione)."""
    if filename in KEEP_EXCEPTIONS:
        return False
        
    try:
        # Controlliamo i primi 5 messaggi della sessione
        msgs = []
        if isinstance(data, dict) and "messages" in data:
            msgs = data["messages"][:5]
        else:
            history = data.get("history", []) if isinstance(data, dict) else data
            if isinstance(history, list):
                msgs = history[:5]
        
        for m in msgs:
            content = str(m).upper()
            if "KNOWLEDGE EXTRACTION PROTOCOL" in content or "CHIEF ARCHIVIST" in content:
                return True
    except:
        pass
    return False

def get_file_priority(file1, file2):
    try:
        s1, s2 = file1.stat(), file2.stat()
        if s1.st_mtime > s2.st_mtime: return True
        if s1.st_mtime == s2.st_mtime and s1.st_size > s2.st_size: return True
    except:
        pass
    return False

def gather_sessions():
    CHATS_DIR.mkdir(parents=True, exist_ok=True)
    logger.info(f"🔍 Avvio rastrellamento chirurgico (v5-msg deep scan)...")
    count_new, count_updated, count_skipped = 0, 0, 0
    
    seen_basenames = set()

    for root in SEARCH_ROOTS:
        if not root.exists(): continue
        for gemini_dir in root.rglob('.gemini'):
            g_path_str = str(gemini_dir).lower()
            
            # Filtro loop: non rileggere l'archivio MD stesso
            if "/workspace/chats/md" in g_path_str:
                continue
            
            if not gemini_dir.is_dir(): continue
            for pattern in ['**/session*.json', '**/checkpoint*.json']:
                for src_path in gemini_dir.glob(pattern):
                    if not src_path.is_file(): continue
                    
                    bname = src_path.name
                    if bname in seen_basenames:
                        count_skipped += 1
                        continue
                    seen_basenames.add(bname)

                    dest_path = CHATS_DIR / bname
                    if dest_path.exists():
                        if get_file_priority(src_path, dest_path):
                            shutil.copy2(src_path, dest_path)
                            count_updated += 1
                    else:
                        shutil.copy2(src_path, dest_path)
                        count_new += 1
    
    logger.info(f"✨ Fine rastrellamento. Nuovi: {count_new}, Aggiornati: {count_updated}, Duplicati ignorati: {count_skipped}")

def transform_json_to_md(json_path, target_dir):
    """Trasforma un JSON in Markdown. Versione Chirurgica Profonda."""
    json_path = Path(json_path)
    orig_size_kb = json_path.stat().st_size / 1024
    
    if (orig_size_kb / 1024) > MAX_JSON_SIZE_MB:
        return None

    try:
        with open(json_path, 'r', encoding='utf-8', errors='replace') as f:
            data = json.load(f)
    except Exception as e:
        return None

    # Filtro Semantico Profondo
    if is_meta_session(data, json_path.name):
        return None

    session_id = data.get("sessionId", "unknown") if isinstance(data, dict) else "unknown"
    start_time = data.get("startTime", "unknown") if isinstance(data, dict) else "unknown"
    
    md_header = [f"# Session: {session_id}", f"- **Start**: {start_time}", f"- **Source**: {json_path.name}\n", "---\n"]
    md_body = []
    msg_processed = 0
    
    if isinstance(data, dict) and "messages" in data:
        for msg in data.get("messages", []):
            try:
                m_type = msg.get("type", "unknown")
                ts = msg.get("timestamp", "N/A")
                text_raw = extract_text_content(msg.get("content", ""))
                text = smart_truncate(clean_protocol_noise(text_raw))

                if m_type == "user":
                    if text:
                        md_body.append(f"### 👤 USER [{ts}]\n{clean_text(text)}\n")
                
                elif m_type in ["assistant", "gemini"]:
                    md_body.append(f"### 🤖 ASSISTANT [{ts}]")
                    for call in (msg.get("tool_calls", []) or msg.get("toolCalls", [])):
                        f_name = call.get("function_name") or call.get("name") or "unknown"
                        args = call.get("arguments") or call.get("args") or {}
                        md_body.append(f"**🛠️ Call: `{f_name}`**")
                        cmd = args.get('command') if isinstance(args, dict) else None
                        md_body.append(f"```bash\n{smart_truncate(cmd if cmd else json.dumps(args, indent=1), 1000)}\n```")
                    
                    if text:
                        md_body.append(f"{clean_text(text)}\n")
                
                elif m_type == "tool":
                    f_name = msg.get("function_name", "unknown")
                    output = extract_text_content(msg.get("content", ""))
                    md_body.append(f"**📦 Tool `{f_name}`**:\n```text\n{clean_text(smart_truncate(output))}\n```\n")
                
                msg_processed += 1
            except: pass
    else:
        history = data.get("history", []) if isinstance(data, dict) else data
        if isinstance(history, list):
            for entry in history:
                try:
                    role, parts = entry.get("role", "unknown"), entry.get("parts", [])
                    if role == "user":
                        for p in parts:
                            if "text" in p:
                                text = smart_truncate(clean_protocol_noise(p["text"]))
                                if text:
                                    md_body.append(f"### 👤 USER\n{clean_text(text)}\n")
                            elif "functionResponse" in p:
                                resp = p["functionResponse"]
                                output = str(resp.get("response", {}).get("output", ""))
                                md_body.append(f"**📦 Tool `{resp.get('name')}`**:\n```text\n{clean_text(smart_truncate(output))}\n```\n")

                    elif role in ["model", "assistant"]:
                        md_body.append(f"### 🤖 ASSISTANT")
                        for p in parts:
                            if "text" in p: 
                                text = smart_truncate(clean_protocol_noise(p["text"]))
                                if text: md_body.append(f"{clean_text(text)}\n")
                            elif "functionCall" in p:
                                call = p["functionCall"]
                                md_body.append(f"**🛠️ Call: `{call.get('name')}`**")
                                cmd = call.get("args", {}).get("command")
                                md_body.append(f"```bash\n{smart_truncate(cmd if cmd else json.dumps(call.get('args'), indent=1), 1000)}\n```")
                    msg_processed += 1
                except: pass

    full_body = "\n".join(md_body)
    if len(full_body.strip()) < MIN_MD_CHAR_COUNT:
        return None

    target_dir.mkdir(parents=True, exist_ok=True)
    md_path = target_dir / json_path.with_suffix(".md").name
    try:
        with open(md_path, 'w', encoding='utf-8') as f:
            f.write("\n".join(md_header + md_body))
        
        new_size_kb = md_path.stat().st_size / 1024
        reduction = 100 - (new_size_kb / orig_size_kb * 100) if orig_size_kb > 0 else 0
        logger.info(f"✅ Success: {json_path.name} ({orig_size_kb:.1f}KB) -> {md_path.name} ({new_size_kb:.1f}KB). Reduction: {reduction:.1f}%")
        return md_path
    except Exception as e:
        logger.error(f"❌ Errore scrittura file MD {md_path.name}: {e}")
        return None

def sync_cycle():
    logger.info("🎬 Start Chronicler Sync (Chirurgico Profondo)")
    gather_sessions()
    json_files = list(CHATS_DIR.glob("*.json"))
    new_conversions = 0
    for j_path in json_files:
        if transform_json_to_md(j_path, MD_DIR): new_conversions += 1
    logger.info(f"🏁 Done. Created/Updated {new_conversions} MD files.")

if __name__ == "__main__":
    if len(sys.argv) > 1:
        src = Path(sys.argv[1])
        if src.is_file(): transform_json_to_md(src, MD_DIR)
        else: logger.error(f"Not found: {sys.argv[1]}")
    else: sync_cycle()
