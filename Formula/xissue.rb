class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.6.1"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.6.1/xissue-v0.6.1-darwin-arm64.tar.gz"
      sha256 "46757915a1def74e259f33b5e93d289aabc26e6157034eff4e1f29f7461e007d"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.6.1/xissue-v0.6.1-darwin-amd64.tar.gz"
      sha256 "d6105644de45623d94b0c34ad63b1684dd13f13fec4716d8019baa8ccc21f1b1"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.6.1/xissue-v0.6.1-linux-arm64.tar.gz"
      sha256 "8fd06151ef3043816701f0e8ccd32879c4d10244cf61b00b522135ff34ba1ab0"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.6.1/xissue-v0.6.1-linux-amd64.tar.gz"
      sha256 "9ba9461916d08847e65c157627b5a39ed5bfaf5f4dbd7453da13054b9f90ad74"
    end
  end

  def install
    bin.install "xissue"
    (pkgshare/"scripts").install Dir["scripts/*"] if Dir.exist?("scripts")
    (pkgshare/"deploy").install Dir["deploy/*"] if Dir.exist?("deploy")
  end

  service do
    run [opt_bin/"xissue"]
    keep_alive true
    working_dir Dir.home
    environment_variables XISSUE_SERVICE: "1"
    log_path var/"log/xissue.log"
    error_log_path var/"log/xissue.log"
  end

  def caveats
    <<~EOS
      启动后台常驻服务方式（二选一）：
      1. Homebrew 服务管理:
         brew services start ffqa/tap/xissue
      2. xIssue 自管服务 (推荐，开机自启):
         xissue service install
    EOS
  end

  test do
    assert_match "xissue", shell_output("#{bin}/xissue version")
  end
end
