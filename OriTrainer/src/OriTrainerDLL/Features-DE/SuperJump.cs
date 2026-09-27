using System;

namespace OriTrainerDLL.Features
{
    // 超级跳：持续把 5 个跳跃高度字段放大到原值的 Multiplier 倍。
    //
    // 为什么是 5 个字段：跳跃不是单一变量，PerformJump 按情形分派 ——
    //   站立跳 / 贴墙跳 / 移动跳 1、2 段 -> FirstJumpHeight
    //   站立跳 / 移动跳 2 段             -> SecondJumpHeight
    //   站立跳 / 移动跳 3 段             -> ThirdJumpHeight
    //   蹲跳                             -> CrouchJumpHeight
    //   转身后空翻                       -> BackflipJumpHeight
    // 只放大 FirstJumpHeight 会"有的跳得高、有的照旧"。
    //
    // JumpIdleHeight 虽在字段表里，但全程序集无人读取，写了没有任何效果。
    // JumpImpulse 量纲不同（冲量而非高度），放大易过冲穿图，故不动。
    //
    // 为什么持续写而不是写一次：跳跃高度虽然运行时无人改写（只有 .ctor 赋值），
    // 但组件本身可能被游戏重建（能力由 SeinPrefabSet 的预制体按需实例化），
    // 重建后新实例是预制体默认值，一次性写入会丢。持续写顺带解决"启动时
    // 还没读档、Sein 为 null 无法捕获原值"的问题。
    //
    // 为什么挂主线程钩子而不是定时器：原版游戏（Unity 5.0 内置的旧 Mono 2.x）里
    // 注入 DLL 的 System.Threading.Timer 不可靠（回调不触发，实测），所以统一改挂
    // 游戏自己的每帧回调 OnGameFixedUpdate
    public static class SuperJump
    {
        private const float Multiplier = 2.5f;

        private static Action _hook; // 保留引用以便 Stop 时注销

        private static SeinJump _jump; // 上次捕获原值时的实例，用于识别组件重建
        private static float _first, _second, _third, _crouch, _backflip;

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

            // 还原跳跃高度原值。只写 float 字段（不是 Unity API），命令线程可直接做。
            try { Restore(); }
            catch { }
        }

        // 由游戏主线程每个 FixedUpdate 调用
        private static void OnGameFixedUpdate()
        {
            // 游戏回调里抛出的异常会顺着 GameController.FixedUpdate 冒到 Unity，
            // 后果不可预期，必须自己兜住
            try
            {
                SeinJump jump = Current();
                if (jump == null) return;

                // 首次拿到 / 组件被重建：按当前实例捕获原值。
                // 新实例是预制体默认值，与旧实例一致，所以重新捕获是安全的。
                // 用 Unity 的 != 而不是 ReferenceEquals：前者能正确识别"旧实例已销毁"，
                // 后者在销毁对象的内存被新对象复用时可能误判为同一个。
                if (jump != _jump)
                {
                    _jump = jump;
                    _first = jump.FirstJumpHeight;
                    _second = jump.SecondJumpHeight;
                    _third = jump.ThirdJumpHeight;
                    _crouch = jump.CrouchJumpHeight;
                    _backflip = jump.BackflipJumpHeight;
                }

                jump.FirstJumpHeight = _first * Multiplier;
                jump.SecondJumpHeight = _second * Multiplier;
                jump.ThirdJumpHeight = _third * Multiplier;
                jump.CrouchJumpHeight = _crouch * Multiplier;
                jump.BackflipJumpHeight = _backflip * Multiplier;
            }
            catch { }
        }

        private static void Restore()
        {
            // 从未成功捕获过原值就还原，会把 0 写进跳跃高度（跳不起来），必须挡住
            if (_jump == null) return;

            SeinJump jump = Current();
            if (jump == null) return;

            jump.FirstJumpHeight = _first;
            jump.SecondJumpHeight = _second;
            jump.ThirdJumpHeight = _third;
            jump.CrouchJumpHeight = _crouch;
            jump.BackflipJumpHeight = _backflip;
            _jump = null;
        }

        // 主菜单 / 读档过程中 Sein 或组件为 null，逐层判空
        private static SeinJump Current()
        {
            SeinCharacter sein = Game.Characters.Sein;
            if (sein == null) return null;

            SeinAbilities abilities = sein.Abilities;
            if (abilities == null) return null;

            return abilities.Jump;
        }
    }
}
