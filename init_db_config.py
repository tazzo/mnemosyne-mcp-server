import requests
import json
import os

SERVER_URL = "http://192.168.1.240:8004"
BLUEPRINT_PATH = "/workspace/tazpod/memory/extraction_blueprint.md"

def init_blueprint():
    if not os.path.exists(BLUEPRINT_PATH):
        print(f"❌ Blueprint file not found at {BLUEPRINT_PATH}")
        return

    with open(BLUEPRINT_PATH, "r") as f:
        blueprint_content = f.read()

    print(f"🚀 Initializing V9 Blueprint at {SERVER_URL}...")
    
    payload = {
        "jsonrpc": "2.0",
        "id": 1,
        "method": "tools/call",
        "params": {
            "name": "update_blueprint",
            "arguments": {"content": blueprint_content}
        }
    }

    try:
        response = requests.post(f"{SERVER_URL}/message?sessionId=init-session", json=payload, timeout=10)
        print(f"✅ Tool call status: {response.status_code}")
        
        # Verifica tramite risorsa
        print("\n--- Verifying resource accessibility ---")
        verify_payload = {
            "jsonrpc": "2.0",
            "id": 2,
            "method": "resources/read",
            "params": {"uri": "resource://mnemosyne/blueprint"}
        }
        # Nota: In SSE la risposta non è nel body della POST, ma ci accontentiamo del 202
        res = requests.post(f"{SERVER_URL}/message?sessionId=init-session", json=verify_payload, timeout=10)
        print(f"✅ Resource request status: {res.status_code}")
        
    except Exception as e:
        print(f"❌ Initialization failed: {e}")

if __name__ == "__main__":
    init_blueprint()
