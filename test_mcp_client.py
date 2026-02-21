import requests
import json
import uuid
import threading
import time
import sys

SERVER_URL = "http://192.168.1.240:8004"
SESSION_ID = str(uuid.uuid4())

def sse_listener(session_id):
    print(f"👂 Starting SSE listener for session {session_id}...")
    try:
        response = requests.get(f"{SERVER_URL}/sse?sessionId={session_id}", stream=True, timeout=30)
        for line in response.iter_lines():
            if line:
                decoded_line = line.decode('utf-8')
                print(f"📥 SSE Received: {decoded_line}")
    except Exception as e:
        print(f"❌ SSE Listener Error: {e}")

def test_mcp():
    print(f"🚀 Testing Mnemosyne MCP Server at {SERVER_URL}")
    print(f"🆔 Session ID: {SESSION_ID}")

    # Start SSE listener in a background thread
    listener_thread = threading.Thread(target=sse_listener, args=(SESSION_ID,), daemon=True)
    listener_thread.start()
    
    # Wait for listener to establish connection
    time.sleep(2)

    # 1. Test Tools List
    print("\n--- Testing 'tools/list' ---")
    payload = {
        "jsonrpc": "2.0",
        "id": 1,
        "method": "tools/list",
        "params": {}
    }
    try:
        response = requests.post(f"{SERVER_URL}/message?sessionId={SESSION_ID}", json=payload, timeout=10)
        print(f"✅ Tools List Request Sent. Status: {response.status_code}")
    except Exception as e:
        print(f"❌ Connection Failed: {e}")
        return

    time.sleep(2)

    # 2. Test Ingest Memory
    print("\n--- Testing 'ingest_memory' ---")
    ingest_payload = {
        "jsonrpc": "2.0",
        "id": 2,
        "method": "tools/call",
        "params": {
            "name": "ingest_memory",
            "arguments": {
                "content": "### TAZLAB MANUAL: MCP DEPLOYMENT\nSuccessfully deployed Mnemosyne MCP server in Go (Distroless) on TazLab Cluster.",
                "timestamp": "2026-02-21T11:00:00Z"
            }
        }
    }
    response = requests.post(f"{SERVER_URL}/message?sessionId={SESSION_ID}", json=ingest_payload, timeout=10)
    print(f"✅ Ingest Request Sent. Status: {response.status_code}")

    time.sleep(5)

    # 3. Test List Memories (to verify ingestion)
    print("\n--- Testing 'list_memories' ---")
    list_payload = {
        "jsonrpc": "2.0",
        "id": 3,
        "method": "tools/call",
        "params": {
            "name": "list_memories",
            "arguments": {"limit": 5}
        }
    }
    response = requests.post(f"{SERVER_URL}/message?sessionId={SESSION_ID}", json=list_payload, timeout=10)
    print(f"✅ List Request Sent. Status: {response.status_code}")

    # Give some time to receive the last response
    time.sleep(5)
    print("\n✨ Test sequence finished.")

if __name__ == "__main__":
    test_mcp()


if __name__ == "__main__":
    test_mcp()
