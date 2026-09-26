using System.Threading;

namespace OriTrainerDLL.Features
{
    // 无限生命：每 10ms 把当前生命写为上限。实现同终极版。
    public static class UnlimitedLife
    {
        private const int IntervalMs = 10;

        private static Timer _timer;

        public static void Start()
        {
            if (_timer != null) return; // 幂等：重复 Start 不重复起定时器

            _timer = new Timer(Refill, null, 0, IntervalMs);
        }

        public static void Stop()
        {
            if (_timer == null) return;

            _timer.Dispose();
            _timer = null;
        }

        private static void Refill(object state)
        {
            // 定时器回调里的未捕获异常会终止整个进程（即游戏），必须自己兜住
            try
            {
                SeinHealthController health = Game.Characters.Sein.Mortality.Health;
                health.Amount = health.MaxHealth;
            }
            catch { }
        }
    }
}
