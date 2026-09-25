// 截图工具：attach 到目标 pid 的控制台，把可见区域逐行读出、以 UTF-8 写文件，退出。
//
// 由 E2E 套件以子进程方式反复调用。它单独成一个进程的原因见 Grab.csproj 的注释
// （编排进程自己来回 AttachConsole 会把进程打崩）。

using System;
using System.IO;
using System.Runtime.InteropServices;
using System.Text;

static class Grab
{
    [DllImport("kernel32.dll", SetLastError = true)]
    static extern bool AttachConsole(uint pid);

    [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
    static extern IntPtr CreateFileW(string name, uint access, uint share, IntPtr sec,
        uint disp, uint flags, IntPtr tmpl);

    // CharSet.Unicode + ExactSpelling 缺一不可：
    //   DllImport 默认 CharSet.Ansi，会把 char[] 按【单字节】封送，而本函数写的是
    //   UTF-16 —— ASCII 每个字符后面跟一个 0、中文被拆成两个字符（实测全是乱码）。
    //   加上 CharSet.Unicode 后 .NET 又会去找 ReadConsoleOutputCharacterWW，
    //   所以必须同时 ExactSpelling=true 锁死函数名。
    [DllImport("kernel32.dll", CharSet = CharSet.Unicode, ExactSpelling = true, SetLastError = true)]
    static extern bool ReadConsoleOutputCharacterW(IntPtr h, [Out] char[] buf, uint len,
        COORD coord, out uint read);

    [DllImport("kernel32.dll")]
    static extern bool GetConsoleScreenBufferInfo(IntPtr h, out CONSOLE_SCREEN_BUFFER_INFO i);

    [DllImport("kernel32.dll")]
    static extern bool FreeConsole();

    [StructLayout(LayoutKind.Sequential)]
    struct COORD { public short X, Y; }
    [StructLayout(LayoutKind.Sequential)]
    struct SMALL_RECT { public short Left, Top, Right, Bottom; }
    [StructLayout(LayoutKind.Sequential)]
    struct CONSOLE_SCREEN_BUFFER_INFO
    {
        public COORD dwSize, dwCursorPosition;
        public ushort wAttributes;
        public SMALL_RECT srWindow;
        public COORD dwMaximumWindowSize;
    }

    static int Main(string[] args)
    {
        if (args.Length < 3) { Console.Error.WriteLine("usage: Grab <pid> <cols> <outfile>"); return 2; }
        int pid = int.Parse(args[0]);
        int width = int.Parse(args[1]);
        string outFile = args[2];

        // 结果先算好、存进内存，最后一次性落盘：避免在 attach 状态下碰 Console。
        StringBuilder outp = new StringBuilder();

        // 必须先脱离自己的控制台：AttachConsole 在本进程已附着控制台时返回
        // ERROR_ACCESS_DENIED(5)，而不是覆盖。
        FreeConsole();

        if (!AttachConsole((uint)pid))
        {
            File.WriteAllText(outFile, "ERR attach failed " + Marshal.GetLastWin32Error());
            return 3;
        }

        IntPtr h = CreateFileW("CONOUT$", 0x80000000 | 0x40000000, 1 | 2,
            IntPtr.Zero, 3, 0, IntPtr.Zero);

        if (h == new IntPtr(-1))
        {
            outp.Append("ERR conout failed ").Append(Marshal.GetLastWin32Error());
        }
        else
        {
            CONSOLE_SCREEN_BUFFER_INFO info;
            if (!GetConsoleScreenBufferInfo(h, out info))
            {
                outp.Append("ERR info failed ").Append(Marshal.GetLastWin32Error());
            }
            else
            {
                int rows = info.srWindow.Bottom - info.srWindow.Top + 1;
                for (int y = 0; y < rows; y++)
                {
                    char[] buf = new char[width];
                    uint read;
                    COORD here;
                    here.X = 0;
                    here.Y = (short)y;
                    if (ReadConsoleOutputCharacterW(h, buf, (uint)width, here, out read))
                        outp.Append(new string(buf, 0, (int)read).TrimEnd());
                    else
                        outp.Append("ERR read failed ").Append(Marshal.GetLastWin32Error());
                    outp.Append('\n');
                }
            }
        }

        FreeConsole();

        File.WriteAllText(outFile, outp.ToString(), new UTF8Encoding(false));
        return 0;
    }
}
