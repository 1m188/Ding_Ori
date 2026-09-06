using System;
using System.Threading;
using System.Windows.Forms;
using OriTrainer.UI;

namespace OriTrainer
{
    internal static class Program
    {
        private static Mutex _mutex;

        [STAThread]
        private static void Main()
        {
            bool createdNew;
            _mutex = new Mutex(true, "OriTrainer_SingleInstance_v1", out createdNew);
            if (!createdNew)
            {
                MessageBox.Show("OriTrainer 已经在运行。", "OriTrainer",
                    MessageBoxButtons.OK, MessageBoxIcon.Information);
                return;
            }

            Application.EnableVisualStyles();
            Application.SetCompatibleTextRenderingDefault(false);
            Application.Run(new MainForm());
            GC.KeepAlive(_mutex);
        }
    }
}
