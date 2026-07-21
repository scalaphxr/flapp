/// Format seconds as m:ss.
pub fn fmt_time(secs: f32) -> String {
    if !secs.is_finite() || secs <= 0.0 {
        return "0:00".to_string();
    }
    let s = secs as u32;
    format!("{}:{:02}", s / 60, s % 60)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn fmt_time_formats_minutes_seconds() {
        assert_eq!(fmt_time(0.0), "0:00");
        assert_eq!(fmt_time(9.0), "0:09");
        assert_eq!(fmt_time(75.0), "1:15");
        assert_eq!(fmt_time(-1.0), "0:00");
    }
}
