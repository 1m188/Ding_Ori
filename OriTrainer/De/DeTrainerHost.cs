using System;
using System.Diagnostics;
using System.Drawing;
using System.IO;
using System.Threading;
using System.Windows.Forms;
using OriTrainer.Core;

namespace OriTrainer.De
{
    /// <summary>
    /// 终极版（FLiNG）修改器宿主：
    /// 1) 从内嵌资源释放到 %TEMP%\OriTrainer\（必须保留原始文件名——该 exe 内部引用自身文件名）；
    /// 2) 启动子进程，等待主窗口，自动定位到本程序旁边；
    /// 3) Cleanup 在退出时结束子进程并删除临时文件。
    ///
    /// 注：实测（重父级健康实例立即黑屏 + PrintWindow 无内容）该修改器的渲染引擎
    /// 不支持作为子窗口工作，因此不做 SetParent 嵌入，以独立窗口旁挂方式运行。
    /// </summary>
    internal sealed class DeTrainerHost
    {
        private const string ResourceName = "OriTrainer.fling_de_trainer.exe";
        private const string ExeFileName = "Ori and the Blind Forest Definitive Edition v1.0 Plus 13 Trainer.exe";

        private Process _child;
        private IntPtr _childHwnd;
        private string _exePath;

        public bool IsRunning
        {
            get { return _child != null && !_child.HasExited; }
        }

        public string Extract()
        {
            var dir = Path.Combine(Path.GetTempPath(), "OriTrainer");
            Directory.CreateDirectory(dir);
            _exePath = Path.Combine(dir, ExeFileName);

            byte[] data = ReadEmbedded();
            var fi = new FileInfo(_exePath);
            if (!fi.Exists || fi.Length != data.Length)
                File.WriteAllBytes(_exePath, data);
            return _exePath;
        }

        private static byte[] ReadEmbedded()
        {
            var asm = typeof(DeTrainerHost).Assembly;
            using (var s = asm.GetManifestResourceStream(ResourceName))
            {
                if (s == null)
                    throw new InvalidOperationException("未找到内嵌资源 " + ResourceName);
                using (var ms = new MemoryStream())
                {
                    s.CopyTo(ms);
                    return ms.ToArray();
                }
            }
        }

        public bool Launch()
        {
            var psi = new ProcessStartInfo(_exePath)
            {
                WorkingDirectory = Path.GetDirectoryName(_exePath)
            };
            _child = Process.Start(psi);
            return _child != null;
        }

        /// <summary>等待子进程主窗口出现，并把它定位到宿主窗口旁边（放不下则居中）。返回是否找到了窗口。</summary>
        public bool WaitAndPositionNextTo(IntPtr hostWindow)
        {
            _childHwnd = IntPtr.Zero;
            for (int i = 0; i < 60; i++)
            {
                if (!IsRunning) return false;
                IntPtr h;
                try
                {
                    _child.Refresh();
                    h = _child.MainWindowHandle;
                }
                catch (Exception) { return false; }

                if (h != IntPtr.Zero && NativeMethods.IsWindow(h) && NativeMethods.IsWindowVisible(h))
                {
                    _childHwnd = h;
                    break;
                }
                Thread.Sleep(250);
            }
            if (_childHwnd == IntPtr.Zero)
                return false;

            try
            {
                NativeMethods.RECT hostR, childR;
                NativeMethods.GetWindowRect(hostWindow, out hostR);
                NativeMethods.GetWindowRect(_childHwnd, out childR);
                var work = Screen.PrimaryScreen.WorkingArea;

                int cw = childR.Right - childR.Left, ch = childR.Bottom - childR.Top;
                int x = hostR.Right + 8;
                int y = hostR.Top;
                if (x + cw > work.Right)
                {
                    // 旁边放不下：屏幕居中
                    x = work.Left + Math.Max(0, (work.Width - cw) / 2);
                    y = work.Top + Math.Max(0, (work.Height - ch) / 2);
                }
                NativeMethods.SetWindowPos(_childHwnd, IntPtr.Zero, x, y, 0, 0,
                    NativeMethods.SWP_NOSIZE | NativeMethods.SWP_NOZORDER);
                return true;
            }
            catch (Exception)
            {
                return true; // 定位失败不影响运行
            }
        }

        public void Cleanup()
        {
            try
            {
                if (IsRunning)
                {
                    if (_childHwnd != IntPtr.Zero)
                        NativeMethods.PostMessage(_childHwnd, NativeMethods.WM_CLOSE, IntPtr.Zero, IntPtr.Zero);
                    if (!_child.WaitForExit(3000))
                        _child.Kill();
                }
            }
            catch (Exception) { }
            finally
            {
                if (_child != null)
                {
                    try { _child.Dispose(); } catch (Exception) { }
                    _child = null;
                }
                _childHwnd = IntPtr.Zero;
                try
                {
                    if (_exePath != null && File.Exists(_exePath))
                        File.Delete(_exePath);
                }
                catch (Exception) { }
            }
        }
    }
}
