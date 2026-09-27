using System;

namespace OriTrainerDLL.Features
{
    // 获得三把钥匙：把 Keys.GinsoTree / ForlornRuins / MountHoru 三个静态标记保持为 true。
    // 实现同终极版：直接赋值等价于游戏自己捡到钥匙（开门判定读的就是这三个静态 bool）；
    // 必须持续写是因为死亡恢复检查点会把钥匙打回成检查点里的值（SeinWorldState.Instance
    // 在恢复白名单里）；停止不还原（SeinWorldState.Serialize 会持久化这三个字段）。
    //
    // 挂游戏自己的每帧回调 OnGameFixedUpdate（原版旧 Mono 的 System.Threading.Timer
    // 不可靠，回调不触发）。
    public static class GrantKeys
    {
        private static Action _hook; // 保留引用以便 Stop 时注销

        public static void Start()
        {
            if (_hook != null) return; // 幂等：重复 Start 不重复挂载

            // Scheduler 由 GameController 持有，而 GameController.Awake 是单例守卫
            // （Instance 已存在则 Destroy 自身），所以该回调在整个进程内稳定可用。
            GameScheduler scheduler = Game.Events.Scheduler ?? throw new Exception("GameScheduler 尚未就绪（游戏未启动完成），功能无法挂载");

            _hook = OnGameFixedUpdate;
            scheduler.OnGameFixedUpdate.Add(_hook);
        }

        public static void Stop()
        {
            if (_hook == null) return;

            Game.Events.Scheduler.OnGameFixedUpdate.Remove(_hook);
            _hook = null;
        }

        // 由游戏主线程每个 FixedUpdate 调用
        private static void OnGameFixedUpdate()
        {
            // 游戏回调里抛出的异常会顺着 GameController.FixedUpdate 冒到 Unity，
            // 后果不可预期，必须自己兜住
            try
            {
                if (!Sein.World.Keys.GinsoTree) Sein.World.Keys.GinsoTree = true;         // Ginso Tree 门钥匙
                if (!Sein.World.Keys.ForlornRuins) Sein.World.Keys.ForlornRuins = true;   // Forlorn Ruins 门钥匙
                if (!Sein.World.Keys.MountHoru) Sein.World.Keys.MountHoru = true;         // Mount Horu 门钥匙
            }
            catch { }
        }
    }
}