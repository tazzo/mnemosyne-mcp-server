import os
import json
import psycopg2
import re
import time
import sys
from datetime import datetime
from google import genai
from google.genai import types

# --- Configuration (TazLab K8s Local Cluster) ---
DB_HOST = os.getenv("DB_HOST", "192.168.1.241")
DB_PORT = os.getenv("DB_PORT", "5432")
DB_NAME = "mnemosyne" # Database rinato
DB_USER = "mnemosyne"
DB_PASS = "dyUuu54TOA8zGMkc)4JFNLYF" # Password corretta dal secret

# Modello Gemini 2.0 Flash: veloce, economico e perfetto per estrazione.
AI_MODEL = "gemini-2.0-flash" 
EMBEDDING_MODEL = "text-embedding-004" 

CHUNK_SIZE = 15000
OVERLAP = 1000
API_PACE = 4 

def get_api_key():
    return os.getenv("GEMINI_API_KEY")

def get_db_connection():
    return psycopg2.connect(
        host=DB_HOST, 
        port=DB_PORT, 
        database=DB_NAME, 
        user=DB_USER, 
        password=DB_PASS, 
        connect_timeout=10
    )

def call_ai_with_retry(client, model, contents, config=None, retries=2):
    for i in range(retries):
        try:
            print(f"    📡 Gemini {model} ({len(str(contents))} chars)...")
            time.sleep(API_PACE)
            if config:
                return client.models.generate_content(model=model, contents=contents, config=config)
            return client.models.generate_content(model=model, contents=contents)
        except Exception as e:
            if "429" in str(e) or "RESOURCE_EXHAUSTED" in str(e):
                wait = (i + 1) * 30 
                print(f"    ⏳ Rate limit! Aspetto {wait}s...")
                time.sleep(wait)
            else: raise e
    raise Exception("Max retries exceeded for Gemini API")

def get_embedding(text):
    api_key = get_api_key()
    url = f"https://generativelanguage.googleapis.com/v1beta/models/{EMBEDDING_MODEL}:embedContent?key={api_key}"
    payload = {"content": {"parts": [{"text": text}]}}
    try:
        import requests
        res = requests.post(url, json=payload, timeout=20)
        if res.status_code == 200:
            return res.json()['embedding']['values']
        else:
            raise Exception(f"Embedding Error ({res.status_code}): {res.text}")
    except Exception as e: raise e

def parse_session_date(log_file):
    file_match = re.search(r'(\d{4}-\d{2}-\d{2})', os.path.basename(log_file))
    if file_match:
        try: return datetime.strptime(file_match.group(1), "%Y-%m-%d")
        except: pass
    return datetime.fromtimestamp(os.path.getmtime(log_file))

def process_file(conn, log_file):
    fname = os.path.basename(log_file)
    with open(log_file, 'r', encoding='utf-8', errors='replace') as f:
        log_data = f.read()
    
    if not log_data.strip(): return False
    session_date = parse_session_date(log_file)
    print(f"\n🚀 Ingestione: {fname} (Data: {session_date.strftime('%Y-%m-%d')})")
    
    client = genai.Client(api_key=get_api_key())
    start = 0
    total_len = len(log_data)
    cursor = conn.cursor()
    
    try:
        while start < total_len:
            chunk = log_data[start:start+CHUNK_SIZE]
            prompt = "Sei Senior Platform Architect. Estrai fatti tecnici High-Res. Rispondi in JSON array: [{\"context\": \"...\", \"tags\": [\"...\"], \"event\": \"...\"}]"
            
            response = call_ai_with_retry(
                client, 
                AI_MODEL, 
                [prompt, f"LOG:\n{chunk}"], 
                config=types.GenerateContentConfig(response_mime_type='application/json')
            )
            
            facts = json.loads(response.text)
            if not isinstance(facts, list): facts = [facts]
            
            for fact in facts:
                if not isinstance(fact, dict) or not fact.get('event'): continue
                unified = f"{fact.get('context')} | {', '.join(fact.get('tags', []))} | {fact.get('event')}"
                vector = get_embedding(unified)
                cursor.execute(
                    "INSERT INTO memories (timestamp, content, embedding) VALUES (%s, %s, %s)", 
                    (session_date, unified, vector)
                )
                print(f"      ✅ Salvato: {fact.get('context')[:60]}...")
            
            start += (CHUNK_SIZE - OVERLAP)
            if start >= total_len: break
        
        cursor.execute("INSERT INTO archived_files (filename) VALUES (%s) ON CONFLICT DO NOTHING", (fname,))
        conn.commit()
        print(f"🌟 File {fname} archiviato con successo.")
        return True

    except Exception as e:
        conn.rollback()
        print(f"🛑 Ingestione interrotta per errore critico: {e}")
        return False
    finally:
        cursor.close()

def archive_one_file(dir_path):
    print("🎯 Ricerca del prossimo file Markdown...")
    try:
        conn = get_db_connection()
        files = sorted([f for f in os.listdir(dir_path) if f.endswith('.md')])
        for f in files:
            cur = conn.cursor()
            cur.execute("SELECT 1 FROM archived_files WHERE filename = %s", (f,))
            if cur.fetchone():
                cur.close()
                continue
            cur.close()
            
            f_path = os.path.join(dir_path, f)
            if process_file(conn, f_path):
                print(f"✅ Fatto: {f}")
                conn.close()
                return 
            else:
                conn.close()
                sys.exit(1)
        
        print("🏁 Tutti i file sono già stati processati.")
        conn.close()
    except Exception as e:
        print(f"❌ Errore connessione DB: {e}")
        sys.exit(1)

def clean_db():
    print("🗑️ Svuotamento database Mnemosyne...")
    try:
        conn = get_db_connection()
        cur = conn.cursor()
        cur.execute("DELETE FROM memories; DELETE FROM archived_files;")
        conn.commit()
        cur.close()
        conn.close()
        print("✨ Database pulito.")
    except Exception as e:
        print(f"❌ Errore pulizia DB: {e}")

if __name__ == "__main__":
    if len(sys.argv) > 1:
        if sys.argv[1] == "clean":
            clean_db()
        elif sys.argv[1] == "single" and len(sys.argv) > 2:
            c = get_db_connection()
            process_file(c, sys.argv[2])
            c.close()
        elif sys.argv[1] == "next" and len(sys.argv) > 2:
            archive_one_file(sys.argv[2])
        else:
            print("Uso: python3 tazlab_archivist.py [clean | next <dir> | single <file>]")
    else:
        print("Specifica un comando.")
