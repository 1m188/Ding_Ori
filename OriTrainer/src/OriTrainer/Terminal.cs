/*
    终端控制：用 ANSI 转义序列驱动控制台界面。

    只有两件事必须走 Win32，其余（清屏、光标定位/显隐、颜色、备用屏幕缓冲区）全部用
    转义序列，不多引 P/Invoke：
      1. 输出必须用 WriteConsoleW —— 界面全是中文，Console.Write 会先按控制台的当前
         代码页编码，非 UTF-8 代码页下中文会变成 '?'。
      2. 必须打开 ENABLE_VIRTUAL_TERMINAL_PROCESSING —— 否则转义序列会被原样打在
         屏幕上，而不是当控制符处理。

    典型用法：
        Terminal.Enter();
        try { while (...) { ...; Terminal.Render(UI.Build(...)); } }
        finally { Terminal.Leave(); }
*/

using System;
using System.Runtime.InteropServices;
using System.Text;

namespace OriTrainer
{
    internal static class Terminal
    {
        // ESC 一律写 \u001b，不写 \x1b：
        // C# 的 \x 会吃满 1~4 位十六进制（"\x1b0" 是 U+01B0，不是 ESC+'0'）。
        // 现在的序列都是 ESC 后面跟 '['，而 '[' 不是十六进制位才侥幸没出事；
        // 把 ESC 单独拎成一个常量，就不必再记这条规则。
        private const string Esc = "\u001b";

        // ---- 颜色 ----
        public const string Reset = Esc + "[0m";             // 复原成默认前景色
        public const string Title = Esc + "[1;38;5;208m";    // 亮橙
        public const string Box = Esc + "[38;5;240m";        // 灰
        public const string Dim = Esc + "[90m";              // 暗灰
        public const string Text = Esc + "[97m";             // 亮白
        public const string Green = Esc + "[92m";
        public const string Red = Esc + "[91m";
        public const string Yellow = Esc + "[93m";

        // 进入界面模式：切备用屏幕缓冲区 → 隐藏光标 → 关自动换行 → 清屏。
        //
        // 备用缓冲区没有回滚历史，界面在原地刷新，不会一直往下追加内容、把滚动条
        // 越推越远。不支持 ?1049 的宿主会忽略该序列，此时靠 Render 的整屏重写兜底。
        // 关自动换行（?7l）让超宽的行在右边缘被截掉，而不是折行把布局顶乱。
        public static void Enter()
        {
            // 开不了就没法画：与其在屏幕上打出一堆 "[0m" 乱码，不如直接报错
            if (!GetConsoleMode(Output, out uint mode)
                || !SetConsoleMode(Output, mode | EnableVirtualTerminalProcessing))
                throw new Exception("控制台不支持 ANSI 转义序列，修改器界面无法绘制");

            Write(Esc + "[?1049h" + Esc + "[?25l" + Esc + "[?7l");
            Clear();
        }

        // 退出界面模式：还原光标 → 恢复自动换行 → 切回主屏幕缓冲区（还原进入前的画面）。
        public static void Leave()
        {
            ShowCursor();
            Write(Esc + "[?7h" + Esc + "[?1049l");
        }

        public static void HideCursor() { Write(Esc + "[?25l"); }
        public static void ShowCursor() { Write(Esc + "[?25h"); }

        // 清屏并把光标归位到左上角。
        public static void Clear() { Write(Esc + "[2J" + Esc + "[H"); }

        // 输出一帧：光标归位 → 按行重写整屏 → 每行清到行尾。
        //
        // 必须重写窗口的每一行（不足处补空行），并给每行追加 ESC[K（清除该行光标
        // 之后的残留）：只写内容行的话，内容变短时（比如关掉某项功能后状态文本变短）
        // 会留下上一帧的字符，界面看起来就是重叠的。
        public static void Render(string frame)
        {
            string[] lines = frame.TrimEnd('\n').Split('\n');

            // 少写一行：写满整屏后光标停在右下角，再有任何输出都会触发滚动，
            // 界面会整体上移、滚动条跟着乱跳。
            int rows = Math.Max(WindowHeight() - 1, 1);

            StringBuilder sb = new StringBuilder(Esc + "[H");
            for (int i = 0; i < rows; i++)
            {
                if (i > 0) sb.Append('\n');
                if (i < lines.Length) sb.Append(lines[i]);
                sb.Append(Esc + "[K");
            }
            Write(sb.ToString());
        }

        // 可见窗口的行数。取不到就按 25 行估（默认控制台高度），不因此中止绘制。
        private static int WindowHeight()
        {
            if (!GetConsoleScreenBufferInfo(Output, out ConsoleScreenBufferInfo info))
                return 25;
            return Math.Max(info.Window.Bottom - info.Window.Top + 1, 8);
        }

        // 一次把整个字符串交给控制台。string 本身就是 UTF-16，长度即字符数，
        // 交给 CharSet.Unicode 的封送层直接按 UTF-16 传，不必自己再做编码转换。
        private static void Write(string s)
        {
            WriteConsoleW(Output, s, (uint)s.Length, out _, IntPtr.Zero);
        }

        // ---- Win32 ----

        private const int StdOutputHandle = -11; // STD_OUTPUT_HANDLE = (DWORD)-11
        private const uint EnableVirtualTerminalProcessing = 0x0004;

        private static readonly IntPtr Output = GetStdHandle(StdOutputHandle);

        [StructLayout(LayoutKind.Sequential)]
        private struct Coord
        {
            public short X, Y;
        }

        [StructLayout(LayoutKind.Sequential)]
        private struct SmallRect
        {
            public short Left, Top, Right, Bottom;
        }

        [StructLayout(LayoutKind.Sequential)]
        private struct ConsoleScreenBufferInfo
        {
            public Coord Size;
            public Coord Cursor;
            public ushort Attributes;
            public SmallRect Window;
            public Coord MaxWindow;
        }

        [DllImport("kernel32.dll", SetLastError = true)]
        private static extern IntPtr GetStdHandle(int stdHandle);

        [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
        private static extern bool WriteConsoleW(IntPtr handle, string buffer, uint length, out uint written, IntPtr reserved);

        [DllImport("kernel32.dll", SetLastError = true)]
        private static extern bool GetConsoleMode(IntPtr handle, out uint mode);

        [DllImport("kernel32.dll", SetLastError = true)]
        private static extern bool SetConsoleMode(IntPtr handle, uint mode);

        [DllImport("kernel32.dll", SetLastError = true)]
        private static extern bool GetConsoleScreenBufferInfo(IntPtr handle, out ConsoleScreenBufferInfo info);
    }
}
