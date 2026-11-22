# Never Sleep

A tiny Windows app that keeps your laptop awake while it is running, with an optional keep-screen-on toggle.

برنامج صغير لويندوز يخلّي اللابتوب ميتقفلش طول ما هو شغّال، مع اختيار يخلّي الشاشة شغالة كمان.

## Download / التحميل

Grab the latest installer from [Releases](../../releases).

حمّل أحدث نسخة من صفحة [Releases](../../releases).

## Features / المميزات

- One-click sleep prevention - the app starts paused and remembers your choices
  - منع السكون بزرار واحد — البرنامج بيفتح متوقف وبيفتكر اختياراتك
- Optional keep-screen-on mode
  - إبقاء الشاشة شغالة كمان (اختياري)
- Run duration: until stopped, 1 hour, 2 hours, 4 hours, or a custom duration in hours and minutes with a remaining-time countdown
  - مدة التشغيل: حتى الإيقاف، ساعة، ساعتين، ٤ ساعات، أو مدة مخصصة بالساعات والدقائق مع عدّاد متبقي
- "On charger only" option stops prevention automatically on battery
  - اختيار "على الشاحن بس" يوقّف المنع تلقائيًا على البطارية
- System tray mode with a status indicator, and a "Stop and exit" button that restores normal sleep settings
  - تصغير جنب الساعة (system tray) مع مؤشر حالة، وزر "إيقاف وخروج" يرجّع إعدادات السكون الطبيعية
- Optional start with Windows, opens next to the clock
  - تشغيل تلقائي مع ويندوز (اختياري) — يفتح جنب الساعة
- Quick duration selection from the tray menu
  - اختيار المدة السريع من قائمة الأيقونة جنب الساعة
- The app does not bypass the security lock or lid-close behavior - it only prevents idle sleep
  - التطبيق مبيعدّيش قفل الأمان ولا إغلاق الغطا — بيمنع سكون الخمول بس

## Install / التثبيت

Run `Never Sleep Setup 1.3.0.exe` and choose the install location. The app is not digitally signed, so Windows may show a protection prompt; if you get one, send me a screenshot first.

شغّل `Never Sleep Setup 1.3.0.exe` واختار مكان التثبيت. البرنامج غير موقّع رقميًا، فممكن يظهر تنبيه حماية من Windows؛ لو ظهرلك أي تنبيه ابعتلي صورته الأول.

## Build from source / البناء من السورس

```bash
npm install
npm test            # logic unit tests
npx electron-builder --win nsis   # needs wine on Linux for the installer step
```

## Tech / التقنية

Electron (Windows, NSIS installer), no backend, settings stored locally.

## Credits

Ahmed Roza — [Instagram](https://www.instagram.com/ahmed_roza_) · [WhatsApp](https://wa.me/201095064150)
