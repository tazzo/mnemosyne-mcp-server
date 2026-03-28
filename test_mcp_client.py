import requests
import json
import time

SERVER_URL = "http://192.168.1.240:8004/mcp"

def test_mcp_streamable():
    print(f"🚀 Testing Mnemosyne MCP (Streamable HTTP) at {SERVER_URL}")

    # 1. Initialize Session
    print("\n--- Initializing Session ---")
    init_payload = {
        "jsonrpc": "2.0",
        "id": 1,
        "method": "initialize",
        "params": {
            "protocolVersion": "2024-11-05",
            "capabilities": {},
            "clientInfo": {
                "name": "TazLab-Test-Client",
                "version": "1.0.0"
            }
        }
    }
    try:
        response = requests.post(f"{SERVER_URL}/initialize", json=init_payload, timeout=10)
        print(f"Status: {response.status_code}")
        if response.status_code != 200:
            print(f"❌ Initialization Failed: {response.text}")
            return
        
        # Debug: print full response and headers
        print(f"Headers: {dict(response.headers)}")
        init_res = response.json()
        print(f"Body: {json.dumps(init_res, indent=2)}")
        
        print(f"✅ Initialized. Server: {init_res.get('result', {}).get('serverInfo', {}).get('name')}")
        
        # In StreamableHTTP, the session ID is in the Mcp-Session-Id header
        session_id = response.headers.get("Mcp-Session-Id") or response.headers.get("X-Session-Id")
        
        if not session_id:
            print("⚠️ No Session ID found in headers. Attempting calls without it (might fail).")
        else:
            print(f"🆔 Session ID: {session_id}")

        headers = {"Mcp-Session-Id": session_id} if session_id else {}

        # 2. Test Tools List
        print("\n--- Testing 'tools/list' ---")
        list_payload = {
            "jsonrpc": "2.0",
            "id": 2,
            "method": "tools/list",
            "params": {}
        }
        response = requests.post(f"{SERVER_URL}/tools/list", json=list_payload, headers=headers, timeout=10)
        print(f"Status: {response.status_code}")
        if response.status_code == 200:
            print(f"✅ Tools: {json.dumps(response.json(), indent=2)}")
        else:
            print(f"❌ Failed: {response.text}")

        # 3. Test Ingest Memory (Async)
        print("\n--- Testing 'ingest_memory' (Async) ---")
        ingest_payload = {
            "jsonrpc": "2.0",
            "id": 3,
            "method": "tools/call",
            "params": {
                "name": "ingest_memory",
                "arguments": {
                    "content": "### TAZLAB TEST: RELIABILITY VERIFIED\nSuccessful end-to-end test of the asynchronous ingestion pipeline with structured logging.",
                    "timestamp": "2026-03-28T23:45:00Z"
                }
            }
        }
        response = requests.post(f"{SERVER_URL}/tools/call", json=ingest_payload, headers=headers, timeout=10)
        print(f"Status: {response.status_code}")
        if response.status_code == 200:
            print(f"✅ Response: {json.dumps(response.json(), indent=2)}")
        else:
            print(f"❌ Failed: {response.text}")

    except Exception as e:
        print(f"❌ Error: {e}")

    print("\n✨ Test sequence finished.")

if __name__ == "__main__":
    test_mcp_streamable()
