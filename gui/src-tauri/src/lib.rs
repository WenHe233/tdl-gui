use std::{
    env,
    io::{BufRead, BufReader, Write},
    path::PathBuf,
    process::{Child, ChildStdin, Command, Stdio},
    sync::Mutex,
};
use tauri::{menu::{Menu, MenuItem}, tray::TrayIconBuilder, Emitter, Manager, State, WindowEvent};

struct WorkerProcess { child: Child, stdin: Option<ChildStdin> }
impl Drop for WorkerProcess { fn drop(&mut self) { self.stdin.take(); let _ = self.child.wait(); } }
#[derive(Default)] struct WorkerState(Mutex<Option<WorkerProcess>>);

fn cli_path() -> Result<PathBuf, String> {
    if let Ok(value) = env::var("TDL_MEDIA_CLI") { return Ok(PathBuf::from(value)); }
    let name = if cfg!(windows) { "tdl-media.exe" } else { "tdl-media" };
    let beside = env::current_exe().map_err(|e| e.to_string())?.with_file_name(name);
    if beside.exists() { return Ok(beside); }
    let dev = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../bin").join(name);
    if dev.exists() { return Ok(dev); }
    Err(format!("找不到 CLI worker：{}", beside.display()))
}

#[tauri::command]
fn worker_start(app: tauri::AppHandle, state: State<'_, WorkerState>) -> Result<(), String> {
    let mut guard = state.0.lock().map_err(|_| "worker lock poisoned")?;
    if guard.is_some() { return Ok(()); }
    let mut command = Command::new(cli_path()?);
    command.arg("worker").stdin(Stdio::piped()).stdout(Stdio::piped()).stderr(Stdio::piped());
    #[cfg(windows)] { use std::os::windows::process::CommandExt; command.creation_flags(0x08000000); }
    let mut child = command.spawn().map_err(|e| format!("启动 worker 失败: {e}"))?;
    let stdin = child.stdin.take().ok_or("worker stdin unavailable")?;
    let stdout = child.stdout.take().ok_or("worker stdout unavailable")?;
    let stderr = child.stderr.take().ok_or("worker stderr unavailable")?;
    let output_app = app.clone();
    std::thread::spawn(move || for line in BufReader::new(stdout).lines().map_while(Result::ok) { let _ = output_app.emit("worker-message", line); });
    let error_app = app.clone();
    std::thread::spawn(move || for line in BufReader::new(stderr).lines().map_while(Result::ok) { let _ = error_app.emit("worker-stderr", line); });
    *guard = Some(WorkerProcess { child, stdin: Some(stdin) });
    Ok(())
}

#[tauri::command]
fn worker_send(line: String, state: State<'_, WorkerState>) -> Result<(), String> {
    let mut guard = state.0.lock().map_err(|_| "worker lock poisoned")?;
    let worker = guard.as_mut().ok_or("worker is not running")?;
    let stdin = worker.stdin.as_mut().ok_or("worker stdin unavailable")?;
    stdin.write_all(line.as_bytes()).and_then(|_| stdin.write_all(b"\n")).and_then(|_| stdin.flush()).map_err(|e| e.to_string())
}

#[tauri::command]
fn worker_stop(state: State<'_, WorkerState>) -> Result<(), String> {
    let worker = state.0.lock().map_err(|_| "worker lock poisoned")?.take();
    // Dropping stdin delivers EOF. WorkerProcess::drop then waits until the Go
    // worker has sealed its session and checkpointed SQLite before we exit.
    drop(worker);
    Ok(())
}

#[tauri::command] fn hide_main(app: tauri::AppHandle) -> Result<(), String> { app.get_webview_window("main").ok_or("main window missing")?.hide().map_err(|e| e.to_string()) }
#[tauri::command] fn quit_app(app: tauri::AppHandle) { app.exit(0); }

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_opener::init())
        .manage(WorkerState::default())
        .invoke_handler(tauri::generate_handler![worker_start, worker_send, worker_stop, hide_main, quit_app])
        .setup(|app| {
            let show = MenuItem::with_id(app, "show", "显示 TDL Media", true, None::<&str>)?;
            let quit = MenuItem::with_id(app, "quit", "退出", true, None::<&str>)?;
            let menu = Menu::with_items(app, &[&show, &quit])?;
            let mut tray = TrayIconBuilder::new().menu(&menu).tooltip("TDL Media");
            if let Some(icon) = app.default_window_icon() { tray = tray.icon(icon.clone()); }
            tray.on_menu_event(|app, event| match event.id.as_ref() {
                "show" => { if let Some(w) = app.get_webview_window("main") { let _ = w.show(); let _ = w.set_focus(); } },
                "quit" => { if let Some(w) = app.get_webview_window("main") { let _ = w.show(); let _ = w.set_focus(); let _ = w.emit("app-close-requested", ()); } },
                _ => {}
            }).build(app)?;
            let window = app.get_webview_window("main").expect("main window");
            let event_window = window.clone();
            window.on_window_event(move |event| if let WindowEvent::CloseRequested { api, .. } = event { api.prevent_close(); let _ = event_window.emit("app-close-requested", ()); });
            Ok(())
        })
        .run(tauri::generate_context!())
        .expect("error while running TDL Media");
}
