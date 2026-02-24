import os
import shutil
from pathlib import Path

def gather_sessions(source_root, target_dir):
    source_root = Path(source_root).resolve()
    target_dir = Path(target_dir).resolve()
    target_dir.mkdir(parents=True, exist_ok=True)
    
    count = 0
    print(f"🔍 Avvio ricerca ricorsiva in: {source_root}")
    print(f"📁 Destinazione: {target_dir}")
    print("-" * 50)
    
    # Cerchiamo tutte le cartelle .gemini nel sistema a partire da qui
    for gemini_dir in source_root.rglob('.gemini'):
        if not gemini_dir.is_dir():
            continue
            
        print(f"📂 Trovata cartella .gemini in: {gemini_dir.parent}")
        
        # Cerchiamo i pattern richiesti dentro .gemini
        patterns = ['**/session*.json', '**/checkpoint*.json']
        for pattern in patterns:
            for file_path in gemini_dir.glob(pattern):
                if file_path.is_file():
                    # Destinazione di base
                    dest_name = file_path.name
                    dest_path = target_dir / dest_name
                    
                    # Gestione collisioni: se il file esiste già con lo stesso nome,
                    # aggiungiamo il nome della cartella genitore come prefisso
                    if dest_path.exists():
                        parent_name = file_path.parent.name
                        dest_name = f"{parent_name}_{file_path.name}"
                        dest_path = target_dir / dest_name
                    
                    try:
                        shutil.copy2(file_path, dest_path)
                        count += 1
                        print(f"  ✅ Copiato: {dest_name}")
                    except Exception as e:
                        print(f"  ❌ Errore copia {file_path.name}: {e}")
                        
    print("-" * 50)
    print(f"✨ Operazione completata. Raccolti {count} file in {target_dir}")

if __name__ == "__main__":
    import sys
    # Se passato un argomento, lo usiamo come root, altrimenti usiamo la directory corrente (dove si trova l'utente)
    root = sys.argv[1] if len(sys.argv) > 1 else os.getcwd()
    gather_sessions(root, "/workspace/chats")
