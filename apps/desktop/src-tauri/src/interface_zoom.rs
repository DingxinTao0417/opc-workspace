//! Interface font size. The workspace styles use fixed pixel sizes, so the
//! appearance setting scales the trusted main view the way browser zoom does:
//! text and layout grow together and the viewport is recomputed. Embedded
//! browser tabs read this zoom when they are placed and keep their own.

/// The offered steps, in percent; anything else is refused.
const INTERFACE_ZOOM_PERCENTS: [u16; 4] = [90, 100, 110, 125];

fn interface_zoom_factor(percent: u16) -> Result<f64, String> {
    if INTERFACE_ZOOM_PERCENTS.contains(&percent) {
        Ok(f64::from(percent) / 100.0)
    } else {
        Err("界面字号只支持 90%、100%、110% 或 125%".into())
    }
}

#[tauri::command]
pub fn set_interface_zoom(webview: tauri::Webview, percent: u16) -> Result<(), String> {
    let factor = interface_zoom_factor(percent)?;
    webview
        .set_zoom(factor)
        .map_err(|_| "无法调整界面字号".to_string())
}

#[cfg(test)]
mod tests {
    use super::interface_zoom_factor;

    #[test]
    fn accepts_only_the_offered_steps() {
        assert_eq!(interface_zoom_factor(90), Ok(0.9));
        assert_eq!(interface_zoom_factor(100), Ok(1.0));
        assert_eq!(interface_zoom_factor(110), Ok(1.1));
        assert_eq!(interface_zoom_factor(125), Ok(1.25));
        for refused in [0, 50, 95, 120, 150, 1000] {
            assert!(interface_zoom_factor(refused).is_err(), "{refused}");
        }
    }
}
